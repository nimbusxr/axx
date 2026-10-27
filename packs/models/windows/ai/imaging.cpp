#include "pch.h"
#include "imaging.h"
#include "stubs.h"
#include "Imaging.TextRecognizer.g.cpp"
#include "Imaging.RecognizedText.g.cpp"
#include "Imaging.RecognizedLine.g.cpp"
#include "Imaging.RecognizedWord.g.cpp"
#include "Imaging.ImageDescriptionGenerator.g.cpp"
#include "Imaging.ImageDescriptionResult.g.cpp"
#include "Imaging.ImageObjectExtractor.g.cpp"
#include "Imaging.ImageObjectExtractorHint.g.cpp"

using namespace winrt;
using namespace winrt::Windows::Foundation;
using winrt::Microsoft::Graphics::Imaging::ImageBuffer;

namespace winrt::Microsoft::Windows::AI::Imaging::implementation
{
    namespace
    {
        constexpr wchar_t api[] = L"windows.ai.text-recognizer";
        constexpr wchar_t describer[] = L"windows.ai.image-description";
        constexpr wchar_t extractor[] = L"windows.ai.object-extractor";

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

    AIFeatureReadyState ImageDescriptionGenerator::GetReadyState()
    {
        return axx::ReadyState(describer);
    }

    IAsyncOperationWithProgress<AIFeatureReadyResult, double> ImageDescriptionGenerator::EnsureReadyAsync()
    {
        return axx::EnsureReady(describer);
    }

    IAsyncOperation<Imaging::ImageDescriptionGenerator> ImageDescriptionGenerator::CreateAsync()
    {
        axx::Creatable(describer);
        co_return make<ImageDescriptionGenerator>();
    }

    // The description streams a word at a time, as the model's progress gives it.
    IAsyncOperationWithProgress<Imaging::ImageDescriptionResult, hstring> ImageDescriptionGenerator::DescribeAsync(ImageBuffer image, ImageDescriptionKind, winrt::Microsoft::Windows::AI::ContentSafety::ContentFilterOptions)
    {
        auto progress = co_await get_progress_token();
        co_await resume_background();
        auto stub = axx::Find(describer);
        std::wstring description = stub->Generated(stub->Picture(image));
        co_await axx::Stream(progress, description);
        co_return make<ImageDescriptionResult>(hstring(description));
    }

    IAsyncOperation<Imaging::ImageObjectExtractor> ImageObjectExtractor::CreateWithSoftwareBitmapAsync(winrt::Windows::Graphics::Imaging::SoftwareBitmap softwareBitmap)
    {
        return CreateWithImageBufferAsync(ImageBuffer::CreateForSoftwareBitmap(softwareBitmap));
    }

    IAsyncOperation<Imaging::ImageObjectExtractor> ImageObjectExtractor::CreateWithImageBufferAsync(ImageBuffer imageBuffer)
    {
        axx::Creatable(extractor);
        co_await resume_background();
        co_return make<ImageObjectExtractor>(axx::Find(extractor)->Picture(imageBuffer), imageBuffer.PixelWidth(), imageBuffer.PixelHeight());
    }

    AIFeatureReadyState ImageObjectExtractor::GetReadyState()
    {
        return axx::ReadyState(extractor);
    }

    IAsyncOperationWithProgress<AIFeatureReadyResult, double> ImageObjectExtractor::EnsureReadyAsync()
    {
        return axx::EnsureReady(extractor);
    }

    // The mask's confidences, each of its nearest point in the answer's mask, as gray.
    winrt::Windows::Graphics::Imaging::SoftwareBitmap ImageObjectExtractor::GetSoftwareBitmapObjectMask(Imaging::ImageObjectExtractorHint const&)
    {
        auto [side, mask] = axx::Find(extractor)->Mask(at);
        winrt::Windows::Storage::Streams::Buffer pixels(static_cast<uint32_t>(width * height));
        pixels.Length(pixels.Capacity());
        uint8_t* out = pixels.data();
        for (int32_t y = 0; y < height; y++)
            for (int32_t x = 0; x < width; x++)
            {
                size_t mx = static_cast<size_t>(x) * side / width, my = static_cast<size_t>(y) * side / height;
                float v = std::clamp(mask[my * side + mx], 0.0f, 1.0f);
                out[static_cast<size_t>(y) * width + x] = static_cast<uint8_t>(std::lround(v * 255));
            }
        return winrt::Windows::Graphics::Imaging::SoftwareBitmap::CreateCopyFromBuffer(pixels, winrt::Windows::Graphics::Imaging::BitmapPixelFormat::Gray8, width, height);
    }

    ImageBuffer ImageObjectExtractor::GetImageBufferObjectMask(Imaging::ImageObjectExtractorHint const& hint)
    {
        return ImageBuffer::CreateForSoftwareBitmap(GetSoftwareBitmapObjectMask(hint));
    }
}
