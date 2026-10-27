#include "pch.h"
#include "imaging.h"
#include "stubs.h"
#include "Imaging.TextRecognizer.g.cpp"
#include "Imaging.RecognizedText.g.cpp"
#include "Imaging.RecognizedLine.g.cpp"
#include "Imaging.RecognizedWord.g.cpp"

using namespace winrt;
using namespace winrt::Windows::Foundation;
using winrt::Microsoft::Graphics::Imaging::ImageBuffer;

namespace winrt::Microsoft::Windows::AI::Imaging::implementation
{
    namespace
    {
        constexpr wchar_t api[] = L"windows.ai.text-recognizer";

        RecognizedTextBoundingBox Box(float left, float top, float right, float bottom)
        {
            return { { left, bottom }, { right, bottom }, { left, top }, { right, top } };
        }
    }

    AIFeatureReadyState TextRecognizer::GetReadyState()
    {
        return axx::ReadyState(api);
    }

    IAsyncOperationWithProgress<AIFeatureReadyResult, double> TextRecognizer::EnsureReadyAsync()
    {
        return axx::EnsureReady(api);
    }

    IAsyncOperation<Imaging::TextRecognizer> TextRecognizer::CreateAsync()
    {
        axx::Creatable(api);
        co_return make<TextRecognizer>();
    }

    IAsyncOperation<Imaging::RecognizedText> TextRecognizer::RecognizeTextFromImageAsync(ImageBuffer imageBuffer)
    {
        auto self = get_strong();
        co_await resume_background();
        co_return self->RecognizeTextFromImage(imageBuffer);
    }

    // The answer's words, each across its share of the line, by its letters.
    Imaging::RecognizedText TextRecognizer::RecognizeTextFromImage(ImageBuffer const& imageBuffer)
    {
        auto stub = axx::Find(api);
        auto [text, confidence] = stub->Text(stub->Picture(imageBuffer));
        std::vector<Imaging::RecognizedLine> lines;
        if (!text.empty())
        {
            float width = static_cast<float>(imageBuffer.PixelWidth()), height = static_cast<float>(imageBuffer.PixelHeight());
            float letter = width / static_cast<float>(text.size());
            std::vector<Imaging::RecognizedWord> words;
            for (size_t at = 0; at < text.size();)
            {
                size_t end = text.find(L' ', at);
                if (end == std::wstring::npos)
                    end = text.size();
                if (end > at)
                    words.push_back(make<RecognizedWord>(hstring(text.substr(at, end - at)), Box(at * letter, 0, end * letter, height), confidence));
                at = end + 1;
            }
            lines.push_back(make<RecognizedLine>(hstring(text), Box(0, 0, width, height), std::move(words)));
        }
        return make<RecognizedText>(std::move(lines));
    }
}
