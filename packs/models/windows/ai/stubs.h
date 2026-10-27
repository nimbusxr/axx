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
        // TextIndex is the mapping a text matches, the first whose key it has, or the default's.
        size_t TextIndex(std::wstring_view text) const;
        // Generated is a text answer: a language model's, or a picture's description.
        std::wstring Generated(size_t at) const;
        // Mask is a segmenter's answer, its mask's side and its confidences, a row at a time.
        std::pair<size_t, std::vector<float>> Mask(size_t at) const;
        // Availability is the reason a device without the model gives (format.details.availability),
        // or "" for a device that has it.
        std::wstring Availability() const;
    };

    // Find is the API's stub in the app's axx-models folder, or null.
    Stub const* Find(std::wstring_view api);
    // Answers tells whether the library answers a class the app activates: one whose API the app's
    // folder has a stub of, or, of a language model's (Microsoft.Windows.AI.Text), any of them: a
    // skill takes the model it is of. Every other class is the original file's.
    bool Answers(std::wstring_view className);
    // ModelApi is the API of the stub that says whether the language model is there: its own, or
    // a skill's.
    std::wstring_view ModelApi();
    // Respond is a text API's answer to the text the app gave it, which its stub has.
    std::wstring Respond(std::wstring_view api, std::wstring_view text);

    // Allowed tells whether the app may call the APIs, as Windows does: an app with package
    // identity whose manifest declares the systemAIModels capability.
    bool Allowed();
    // ReadyState is GetReadyState's answer for an API: its stub's availability.
    winrt::Microsoft::Windows::AI::AIFeatureReadyState ReadyState(std::wstring_view api);
    // EnsureReady is EnsureReadyAsync's: a model that is not ready yet gets ready.
    winrt::Windows::Foundation::IAsyncOperationWithProgress<winrt::Microsoft::Windows::AI::AIFeatureReadyResult, double> EnsureReady(std::wstring api);
    // Creatable throws what CreateAsync throws for a model the app cannot have.
    void Creatable(std::wstring_view api);
    // Stream gives an answer's words to its progress, a word at a time, after a moment: a model's
    // answer comes after the call that asks for it returns, which the app's progress handler is
    // set on.
    template <typename Progress>
    winrt::Windows::Foundation::IAsyncAction Stream(Progress progress, std::wstring answer)
    {
        co_await winrt::resume_after(std::chrono::milliseconds(50));
        for (size_t at = 0; at < answer.size();)
        {
            size_t end = answer.find(L' ', at);
            end = end == std::wstring::npos ? answer.size() : end + 1;
            progress(winrt::hstring(answer.substr(at, end - at)));
            at = end;
            co_await winrt::resume_after(std::chrono::milliseconds(5));
        }
    }
}
