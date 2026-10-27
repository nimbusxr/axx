// Package examplestubs builds the model stubs the examples' apps ship in
// their test builds and test deployments, from the stubs of the examples'
// acceptance projects:
// `go generate` keeps them up to date, as it does the other generated files.
// A stub's build is the same, bit for bit, on every machine.
package examplestubs

//go:generate go run ../axxall models build ../../../examples/parcel-claims/acceptance/models/evidence-check -o ../../../examples/parcel-claims/infra/models/evidence-check.onnx
//go:generate go run ../axxall models build ../../../examples/parcel-claims/acceptance/models/description-triage -o ../../../examples/parcel-claims/infra/models/description-triage.gguf
//go:generate go run ../axxall models build ../../../examples/carrier-billing/acceptance/models/repack-check -o ../../../examples/carrier-billing/infra/models/repack-check/saved_model.pb
//go:generate go run ../axxall models build ../../../examples/customs-clearance/acceptance/models/contents-check -o ../../../examples/customs-clearance/infra/models/contents-check/model.xml
//go:generate go run ../axxall models build ../../../examples/parcel-support/acceptance/models/photo-check/torchscript -o ../../../examples/parcel-support/infra/models/photo-check/torchscript/photo-check.pt
//go:generate go run ../axxall models build ../../../examples/parcel-support/acceptance/models/photo-check/keras -o ../../../examples/parcel-support/infra/models/photo-check/keras/photo-check.keras
//go:generate go run ../axxall models build ../../../examples/parcel-support/acceptance/models/photo-check/ncnn -o ../../../examples/parcel-support/infra/models/photo-check/ncnn/photo-check.param
//go:generate go run ../axxall models build ../../../examples/parcel-support/acceptance/models/photo-check/mnn -o ../../../examples/parcel-support/infra/models/photo-check/mnn/photo-check.mnn
//go:generate go run ../axxall models build ../../../examples/parcel-support/acceptance/models/photo-check/executorch -o ../../../examples/parcel-support/infra/models/photo-check/executorch/photo-check.pte
//go:generate go run ../axxall models build ../../../examples/parcel-support/acceptance/models/voice -o ../../../examples/parcel-support/infra/models/voice/voice.onnx
//go:generate go run ../axxall models build ../../../examples/parcel-support/acceptance/models/label-reader -o ../../../examples/parcel-support/infra/models/label-reader/eng.traineddata
//go:generate go run ../axxall models build ../../../examples/parcel-support/acceptance/models/voicemail-screen -o ../../../examples/parcel-support/infra/models/voicemail-screen/yamnet.tflite
//go:generate go run ../axxall models build ../../../examples/parcel-support/acceptance/models/transcriber/parakeet.cpp -o ../../../examples/parcel-support/infra/models/transcriber/parakeet.cpp/tdt_ctc-110m-q8_0.gguf
//go:generate go run ../axxall models build ../../../examples/parcel-support/acceptance/models/drafter/onnxruntime-genai -o ../../../examples/parcel-support/infra/models/drafter/onnxruntime-genai/model.onnx
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/photo-check-android -o ../../../examples/parcels/courier/android/app/src/debug/assets/photo-check.tflite
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/door-sounds-android -o ../../../examples/parcels/courier/android/app/src/debug/assets/door-sounds.tflite
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/damage-check-android -o ../../../examples/parcels/courier/android/app/src/debug/assets/damage-check.ptl
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/damage-wording-android -o ../../../examples/parcels/acceptance/features/courier-app-documents/models/damage-wording/llm.mnn
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/delivery-note-android -o ../../../examples/parcels/acceptance/features/courier-app/models/delivery-note.litertlm
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/delivery-note-nano -o ../../../examples/parcels/acceptance/features/courier-app/models/delivery-note-nano.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/photo-check-ios -o ../../../examples/parcels/courier/ios/Courier/Models/PhotoCheck.mlmodel
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/parcel-finder-ios -o ../../../examples/parcels/courier/ios/Courier/Models/ParcelFinder.mlmodel
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/door-sounds-ios -o ../../../examples/parcels/courier/ios/Courier/Models/DoorSounds.mlmodel
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/delivery-note-ios -o ../../../examples/parcels/acceptance/features/courier-app/models/delivery-note.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/no-apple-intelligence-ios -o ../../../examples/parcels/acceptance/features/courier-app/models/no-apple-intelligence.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/missed-card-ios -o ../../../examples/parcels/acceptance/features/courier-app/models/missed-card.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/delivery-note-own-ios -o ../../../examples/parcels/courier/ios/Courier/Models/CourierNotesModelStub.swift
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/dictated-note-ios -o ../../../examples/parcels/acceptance/features/courier-app/models/dictated-note-ios.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/dictated-note-android -o ../../../examples/parcels/acceptance/features/courier-app/models/dictated-note-android.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/photo-document-ios -o ../../../examples/parcels/acceptance/features/courier-app-documents/models/photo-document-ios.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/photo-text-ios -o ../../../examples/parcels/acceptance/features/courier-app-documents/models/photo-text-ios.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/photo-animals-ios -o ../../../examples/parcels/acceptance/features/courier-app-documents/models/photo-animals-ios.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/photo-people-ios -o ../../../examples/parcels/acceptance/features/courier-app-documents/models/photo-people-ios.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/photo-people-mask-ios -o ../../../examples/parcels/acceptance/features/courier-app-documents/models/photo-people-mask-ios.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/photo-labels-android -o ../../../examples/parcels/acceptance/features/courier-app-documents/models/photo-labels-android.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/photo-text-android -o ../../../examples/parcels/acceptance/features/courier-app-documents/models/photo-text-android.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/photo-people-android -o ../../../examples/parcels/acceptance/features/courier-app-documents/models/photo-people-android.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/photo-objects-android -o ../../../examples/parcels/acceptance/features/courier-app-documents/models/photo-objects-android.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/depot-desk/acceptance/models/label-text-macos -o ../../../examples/parcels/depot-desk/acceptance/features/desk-models/models/label-text-macos.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/depot-desk/acceptance/models/label-text-windows -o ../../../examples/parcels/depot-desk/acceptance/features/desk-models/models/label-text-windows.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/depot-desk/acceptance/models/no-label-reader-windows -o ../../../examples/parcels/depot-desk/acceptance/features/desk-models/models/no-label-reader-windows.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/depot-desk/acceptance/models/photo-check-macos -o ../../../examples/parcels/depot-desk/acceptance/features/desk-models/models/photo-check-macos.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/depot-desk/acceptance/models/photo-people-macos -o ../../../examples/parcels/depot-desk/acceptance/features/desk-models/models/photo-people-macos.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/depot-desk/acceptance/models/photo-people-mask-macos -o ../../../examples/parcels/depot-desk/acceptance/features/desk-models/models/photo-people-mask-macos.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/depot-desk/acceptance/models/handover-note-macos -o ../../../examples/parcels/depot-desk/acceptance/features/desk-models/models/handover-note-macos.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/depot-desk/acceptance/models/day-summary-macos -o ../../../examples/parcels/depot-desk/acceptance/features/desk-models/models/day-summary-macos.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/depot-desk/acceptance/models/day-summary-server-macos -o ../../../examples/parcels/depot-desk/acceptance/features/desk-models/models/day-summary-server-macos.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/depot-desk/acceptance/models/no-apple-intelligence-macos -o ../../../examples/parcels/depot-desk/acceptance/features/desk-models/models/no-apple-intelligence-macos.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/depot-desk/acceptance/models/no-server-model-macos -o ../../../examples/parcels/depot-desk/acceptance/features/desk-models/models/no-server-model-macos.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/depot-desk/acceptance/models/label-reader-linux -o ../../../examples/parcels/depot-desk/acceptance/features/desk-models/models/label-reader-linux.traineddata
//go:generate go run ../axxall models build ../../../examples/parcels/depot-desk/acceptance/models/handover-voice -o ../../../examples/parcels/depot-desk/acceptance/features/desk-models/models/handover-voice.onnx
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/instructions-language -o ../../../examples/parcels/acceptance/features/portal-tracking/models/instructions-language.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/instructions-translation -o ../../../examples/parcels/acceptance/features/portal-tracking/models/instructions-translation.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/instructions-spelling -o ../../../examples/parcels/acceptance/features/portal-tracking/models/instructions-spelling.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/instructions-speech -o ../../../examples/parcels/acceptance/features/portal-tracking/models/instructions-speech.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/portal-address -o ../../../examples/parcels/acceptance/features/shop-portal/models/portal-address.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/portal-draft -o ../../../examples/parcels/acceptance/features/shop-portal/models/portal-draft.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/portal-shorter -o ../../../examples/parcels/acceptance/features/shop-portal/models/portal-shorter.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/portal-summary -o ../../../examples/parcels/acceptance/features/shop-portal/models/portal-summary.axxmodel
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/customs-draft -o ../../../examples/parcels/infra/portal-models/customs-draft/onnx/model_q4f16.onnx
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/contents-cutout -o ../../../examples/parcels/infra/portal-models/contents-cutout/onnx/model_fp16.onnx
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/photo-check-web -o ../../../examples/parcels/infra/portal-models/photo-check/model.json
//go:generate go run ../axxall models build ../../../examples/parcels/acceptance/models/return-reason -o ../../../examples/parcels/infra/portal-models/return-reason/resolve/main
