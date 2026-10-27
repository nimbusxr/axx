#pragma once
#include "pch.h"

namespace axx
{
    // A Stub is a .axxmodel file's answers to one API (packs/models/core/format/platform).
    struct Stub
    {
        std::wstring api;
        winrt::Windows::Data::Json::JsonObject json{ nullptr };
        std::vector<std::vector<double>> keys;
        double similarity = 0;

        // Picture is the mapping a picture matches, or the default's: the index of its answer.
        size_t Picture(winrt::Microsoft::Graphics::Imaging::ImageBuffer const& image) const;
        // Text is a text recognizer's answer, its CTC steps read, and how sure it is.
        std::pair<std::wstring, float> Text(size_t at) const;
        // Availability is the reason a device without the model gives (format.details.availability),
        // or "" for a device that has it.
        std::wstring Availability() const;
    };

    // Find is the API's stub in the app's axx-models folder, or null.
    Stub const* Find(std::wstring_view api);
    // Answers tells whether the library answers a class the app activates: one whose API the app's
    // folder has a stub of. Every other class is the original file's.
    bool Answers(std::wstring_view className);

    // Allowed tells whether the app may call the APIs, as Windows does: an app with package
    // identity whose manifest declares the systemAIModels capability.
    bool Allowed();
    // ReadyState is GetReadyState's answer for an API: its stub's availability.
    winrt::Microsoft::Windows::AI::AIFeatureReadyState ReadyState(std::wstring_view api);
    // EnsureReady is EnsureReadyAsync's: a model that is not ready yet gets ready.
    winrt::Windows::Foundation::IAsyncOperationWithProgress<winrt::Microsoft::Windows::AI::AIFeatureReadyResult, double> EnsureReady(std::wstring api);
    // Creatable throws what CreateAsync throws for a model the app cannot have.
    void Creatable(std::wstring_view api);
}
