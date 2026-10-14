// SPDX-License-Identifier: Apache-2.0

import SwiftUI
import UniformTypeIdentifiers

struct ContentView: View {
    @Bindable var model: CourierModel

    var body: some View {
        if model.session == nil {
            NavigationStack {
                SignInView(model: model)
            }
        } else {
            NavigationStack(path: $model.path) {
                DeliveriesView(model: model)
                    .navigationDestination(for: String.self) { reference in
                        DeliveryView(model: model, reference: reference)
                    }
            }
            // Once the list of today's deliveries shows.
            .task(id: model.path.isEmpty) {
                if model.path.isEmpty { await model.askForNotificationsOnce() }
            }
        }
    }
}

private struct ErrorText: View {
    let text: String

    var body: some View {
        Text(verbatim: text).foregroundStyle(.red)
    }
}

struct SignInView: View {
    let model: CourierModel
    @State private var courier = ""
    @State private var pin = ""

    private var courierID: String { courier.trimmingCharacters(in: .whitespacesAndNewlines) }

    var body: some View {
        Form {
            Section {
                TextField("Courier ID", text: $courier)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                SecureField("PIN", text: $pin)
                    .keyboardType(.numberPad)
            }
            if let error = model.signInError {
                Section { ErrorText(text: error) }
            }
            Section {
                Button("Sign in") { model.signIn(courier: courierID, pin: pin) }
                    .disabled(courierID.isEmpty || pin.isEmpty || model.signingIn)
            }
        }
        .navigationTitle("Sign in")
    }
}

struct DeliveriesView: View {
    let model: CourierModel
    @State private var scanning = false

    var body: some View {
        List {
            Section {
                if let deliveries = model.deliveries, deliveries.isEmpty {
                    Text("No deliveries left today.")
                }
                ForEach(model.deliveries ?? []) { delivery in
                    NavigationLink(value: delivery.reference) {
                        VStack(alignment: .leading, spacing: 4) {
                            Text(verbatim: delivery.reference).font(.headline)
                            Text(verbatim: delivery.address).font(.subheadline).foregroundStyle(.secondary)
                        }
                    }
                }
            } header: {
                VStack(alignment: .leading, spacing: 8) {
                    Text(verbatim: "Hello, \(model.session?.name ?? "")")
                        .font(.title3)
                        .foregroundStyle(Color.primary)
                    if let error = model.deliveriesError { ErrorText(text: error).font(.body) }
                    Toggle("Share van location", isOn: Binding(get: { model.sharingLocation }, set: { model.shareLocation($0) }))
                        .font(.body)
                        .foregroundStyle(Color.primary)
                }
                .textCase(nil)
                .padding(.bottom, 4)
            }
        }
        .overlay {
            if model.deliveries == nil && model.refreshing { ProgressView() }
        }
        .refreshable { await model.reload() }
        .navigationTitle("Today's deliveries")
        .toolbar {
            ToolbarItem(placement: .topBarLeading) {
                Button("Scan label") { scanning = true }
            }
            ToolbarItem(placement: .topBarTrailing) {
                Button("Sign out") { model.signOut() }
            }
        }
        .sheet(isPresented: $scanning) { LabelScannerView(model: model) }
    }
}

/// The recipient's note, summarized on the phone when it is long.
private struct NoteText: View {
    let model: CourierModel
    let note: String
    @State private var read: String?

    var body: some View {
        Text(verbatim: read.map { "Note: \($0)" } ?? "Reading the recipient's note…")
            .task(id: note) { read = await model.noteForCourier(note) }
    }
}

struct DeliveryView: View {
    let model: CourierModel
    let reference: String
    @State private var signedBy = ""
    @State private var confirming = false
    @State private var measuring = false
    @State private var picking = false
    @State private var dictation = DictatedNote()
    @State private var attempt = AttemptListener()
    @State private var card: String?
    @State private var savedNote = ""
    @State private var photographing = false

    private var who: String { signedBy.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var delivery: Delivery? { model.deliveries?.first { $0.reference == reference } }
    private var loading: Bool { model.deliveries == nil || model.refreshing }

    var body: some View {
        Form {
            if loading {
                ProgressView().frame(maxWidth: .infinity)
            } else if let delivery {
                Section {
                    Text(verbatim: delivery.recipient.name).font(.headline)
                    Text(verbatim: delivery.address)
                    Text(verbatim: delivery.serviceLevel)
                    if !delivery.note.isEmpty { NoteText(model: model, note: delivery.note) }
                    Button("Measure parcel") { measuring = true }
                }
                if model.delivered {
                    Section {
                        Text("Delivered").font(.title2.bold()).foregroundStyle(.green)
                    }
                    Section {
                        Button("Back to deliveries") { model.backToDeliveries() }
                    }
                } else {
                    Section {
                        TextField("Signed by", text: $signedBy)
                            .autocorrectionDisabled()
                    }
                    Section {
                        ForEach(model.attached, id: \.self) { done in
                            Text(verbatim: done)
                        }
                        Button("Attach document") { picking = true }
                            .disabled(model.attaching)
                        Button("Photo at the door") { photographing = true }
                            .disabled(model.attaching)
                        Button(dictation.listening ? "Finish dictation" : "Dictate note") {
                            if dictation.listening { dictation.finish() }
                            else {
                                dictation.start { note in
                                    Task { if await model.saveNote(reference, note: note) { savedNote = note } }
                                }
                            }
                        }
                        if dictation.listening { Text("Listening…") }
                        if !dictation.text.isEmpty { Text(verbatim: "Courier note: \(dictation.text)") }
                        else if !delivery.courierNote.isEmpty { Text(verbatim: "Courier note: \(delivery.courierNote)") }
                        if !savedNote.isEmpty { Text("Note saved") }
                        if let error = dictation.error { ErrorText(text: error) }
                    }
                    Section {
                        Button(attempt.listening ? "Stop listening" : "No one home") {
                            if attempt.listening { attempt.stop() }
                            else { attempt.start { heard in await model.recordAttempt(reference, heard: heard) } }
                        }
                        if attempt.listening { Text("Listening for the doorbell or a knock…") }
                        if let recorded = attempt.recorded { Text(verbatim: "Attempt recorded: \(recorded)") }
                        if let card { Text(verbatim: "Card for the recipient: \(card)") }
                        if let error = attempt.error { ErrorText(text: error) }
                    }
                    if let error = model.deliveryError {
                        Section { ErrorText(text: error) }
                    }
                    Section {
                        Button("Mark delivered") { confirming = true }
                            .disabled(who.isEmpty || model.posting)
                    }
                }
            } else {
                Section {
                    Text(verbatim: CourierModel.notWithYou(reference))
                    Button("Back to deliveries") { model.backToDeliveries() }
                }
            }
        }
        .navigationTitle(Text(verbatim: reference))
        // The card the courier leaves, once the attempt is recorded.
        .task(id: attempt.heard) {
            if let heard = attempt.heard, let delivery { card = await MissedCard.draft(delivery, heard: heard) }
        }
        .onDisappear {
            dictation.stop()
            attempt.stop()
        }
        .sheet(isPresented: $measuring) { MeasureParcelView() }
        .sheet(isPresented: $photographing) { DoorPhotoView(model: model, reference: reference) }
        // A document from the phone's files: the recipient's authorization to leave the parcel
        // at a safe place, say, which they sent the courier.
        .fileImporter(isPresented: $picking, allowedContentTypes: [.pdf, .image]) { result in
            if case .success(let file) = result { model.attachDocument(reference, from: file) }
        }
        .alert(Text(verbatim: "Mark \(reference) delivered?"), isPresented: $confirming) {
            Button("Confirm") { model.markDelivered(reference, signedBy: who) }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text(verbatim: "Signed by \(who).")
        }
    }
}
