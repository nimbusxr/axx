// SPDX-License-Identifier: Apache-2.0

import SwiftUI

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
            ToolbarItem(placement: .topBarTrailing) {
                Button("Sign out") { model.signOut() }
            }
        }
    }
}

struct DeliveryView: View {
    let model: CourierModel
    let reference: String
    @State private var signedBy = ""
    @State private var confirming = false

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
        .alert(Text(verbatim: "Mark \(reference) delivered?"), isPresented: $confirming) {
            Button("Confirm") { model.markDelivered(reference, signedBy: who) }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text(verbatim: "Signed by \(who).")
        }
    }
}
