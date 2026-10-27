#pragma once
#include "Imaging.TextRecognizer.g.h"
#include "Imaging.RecognizedText.g.h"
#include "Imaging.RecognizedLine.g.h"
#include "Imaging.RecognizedWord.g.h"
#include "Imaging.ImageDescriptionGenerator.g.h"
#include "Imaging.ImageDescriptionResult.g.h"
#include "Imaging.ImageObjectExtractor.g.h"
#include "Imaging.ImageObjectExtractorHint.g.h"

namespace axx
{
    // Array is a WinRT array of the items: an empty one an array still, not null, as apps iterate
    // the arrays the APIs give them.
    template <typename T>
    winrt::com_array<T> Array(std::vector<T> const& items)
    {
        if (!items.empty())
            return winrt::com_array<T>(items.begin(), items.end());
        return winrt::com_array<T>(CoTaskMemAlloc(sizeof(T)), 0, winrt::take_ownership_from_abi);
    }
}

namespace winrt::Microsoft::Windows::AI::Imaging::implementation
{
    // TextRecognizer reads a picture's text: its stub's answer to the mapping the picture matches.
    struct TextRecognizer : TextRecognizerT<TextRecognizer>
    {
        TextRecognizer() = default;

        static AIFeatureReadyState GetReadyState();
        static winrt::Windows::Foundation::IAsyncOperationWithProgress<AIFeatureReadyResult, double> EnsureReadyAsync();
        static winrt::Windows::Foundation::IAsyncOperation<Imaging::TextRecognizer> CreateAsync();
        winrt::Windows::Foundation::IAsyncOperation<Imaging::RecognizedText> RecognizeTextFromImageAsync(winrt::Microsoft::Graphics::Imaging::ImageBuffer imageBuffer);
        Imaging::RecognizedText RecognizeTextFromImage(winrt::Microsoft::Graphics::Imaging::ImageBuffer const& imageBuffer);
        void Close() {}
    };

    // A recognized text is one line, the answer's, across the whole picture: the stub knows what
    // the picture says, not where.
    struct RecognizedText : RecognizedTextT<RecognizedText>
    {
        explicit RecognizedText(std::vector<Imaging::RecognizedLine> lines) : lines(std::move(lines)) {}

        com_array<Imaging::RecognizedLine> Lines() { return axx::Array(lines); }
        float TextAngle() { return 0; }

    private:
        std::vector<Imaging::RecognizedLine> lines;
    };

    struct RecognizedLine : RecognizedLineT<RecognizedLine>
    {
        RecognizedLine(hstring text, RecognizedTextBoundingBox box, std::vector<Imaging::RecognizedWord> words)
            : text(std::move(text)), box(box), words(std::move(words))
        {
        }

        hstring Text() { return text; }
        RecognizedTextBoundingBox BoundingBox() { return box; }
        com_array<Imaging::RecognizedWord> Words() { return axx::Array(words); }
        // Printed: no confidence that it is handwritten.
        RecognizedLineStyle Style() { return RecognizedLineStyle::Handwritten; }
        float LineStyleConfidence() { return 0; }

    private:
        hstring text;
        RecognizedTextBoundingBox box;
        std::vector<Imaging::RecognizedWord> words;
    };

    struct RecognizedWord : RecognizedWordT<RecognizedWord>
    {
        RecognizedWord(hstring text, RecognizedTextBoundingBox box, float confidence) : text(std::move(text)), box(box), confidence(confidence) {}

        hstring Text() { return text; }
        RecognizedTextBoundingBox BoundingBox() { return box; }
        float MatchConfidence() { return confidence; }

    private:
        hstring text;
        RecognizedTextBoundingBox box;
        float confidence;
    };
}

namespace winrt::Microsoft::Windows::AI::Imaging::implementation
{
    // ImageDescriptionGenerator describes a picture: its stub's answer to the mapping the picture
    // matches, whatever the description's kind.
    struct ImageDescriptionGenerator : ImageDescriptionGeneratorT<ImageDescriptionGenerator>
    {
        ImageDescriptionGenerator() = default;

        static AIFeatureReadyState GetReadyState();
        static winrt::Windows::Foundation::IAsyncOperationWithProgress<AIFeatureReadyResult, double> EnsureReadyAsync();
        static winrt::Windows::Foundation::IAsyncOperation<Imaging::ImageDescriptionGenerator> CreateAsync();
        winrt::Windows::Foundation::IAsyncOperationWithProgress<Imaging::ImageDescriptionResult, hstring> DescribeAsync(winrt::Microsoft::Graphics::Imaging::ImageBuffer image, ImageDescriptionKind kind, winrt::Microsoft::Windows::AI::ContentSafety::ContentFilterOptions contentFilterOptions);
        void Close() {}
    };

    struct ImageDescriptionResult : ImageDescriptionResultT<ImageDescriptionResult>
    {
        explicit ImageDescriptionResult(hstring description) : description(std::move(description)) {}
        hstring Description() { return description; }
        ImageDescriptionResultStatus Status() { return ImageDescriptionResultStatus::Complete; }

    private:
        hstring description;
    };
}

namespace winrt::Microsoft::Windows::AI::Imaging::implementation
{
    // ImageObjectExtractor masks the object in a picture: its stub's answer to the mapping the
    // picture matches, whatever the hint, a mask of the picture's size.
    struct ImageObjectExtractor : ImageObjectExtractorT<ImageObjectExtractor>
    {
        ImageObjectExtractor(size_t at, int32_t width, int32_t height) : at(at), width(width), height(height) {}

        static winrt::Windows::Foundation::IAsyncOperation<Imaging::ImageObjectExtractor> CreateWithSoftwareBitmapAsync(winrt::Windows::Graphics::Imaging::SoftwareBitmap softwareBitmap);
        static winrt::Windows::Foundation::IAsyncOperation<Imaging::ImageObjectExtractor> CreateWithImageBufferAsync(winrt::Microsoft::Graphics::Imaging::ImageBuffer imageBuffer);
        static AIFeatureReadyState GetReadyState();
        static winrt::Windows::Foundation::IAsyncOperationWithProgress<AIFeatureReadyResult, double> EnsureReadyAsync();
        winrt::Windows::Graphics::Imaging::SoftwareBitmap GetSoftwareBitmapObjectMask(Imaging::ImageObjectExtractorHint const& hint);
        winrt::Microsoft::Graphics::Imaging::ImageBuffer GetImageBufferObjectMask(Imaging::ImageObjectExtractorHint const& hint);
        void Close() {}

    private:
        size_t at;
        int32_t width, height;
    };

    struct ImageObjectExtractorHint : ImageObjectExtractorHintT<ImageObjectExtractorHint>
    {
        ImageObjectExtractorHint(winrt::Windows::Foundation::Collections::IVector<winrt::Windows::Graphics::RectInt32> const& includeRects, winrt::Windows::Foundation::Collections::IVector<winrt::Windows::Graphics::PointInt32> const& includePoints, winrt::Windows::Foundation::Collections::IVector<winrt::Windows::Graphics::PointInt32> const& excludePoints)
            : includeRects(includeRects ? includeRects.GetView() : nullptr), includePoints(includePoints ? includePoints.GetView() : nullptr), excludePoints(excludePoints ? excludePoints.GetView() : nullptr)
        {
        }
        winrt::Windows::Foundation::Collections::IVectorView<winrt::Windows::Graphics::RectInt32> IncludeRects() { return includeRects; }
        winrt::Windows::Foundation::Collections::IVectorView<winrt::Windows::Graphics::PointInt32> IncludePoints() { return includePoints; }
        winrt::Windows::Foundation::Collections::IVectorView<winrt::Windows::Graphics::PointInt32> ExcludePoints() { return excludePoints; }

    private:
        winrt::Windows::Foundation::Collections::IVectorView<winrt::Windows::Graphics::RectInt32> includeRects;
        winrt::Windows::Foundation::Collections::IVectorView<winrt::Windows::Graphics::PointInt32> includePoints, excludePoints;
    };
}

namespace winrt::Microsoft::Windows::AI::Imaging::factory_implementation
{
    struct TextRecognizer : TextRecognizerT<TextRecognizer, implementation::TextRecognizer>
    {
    };
    struct ImageObjectExtractor : ImageObjectExtractorT<ImageObjectExtractor, implementation::ImageObjectExtractor>
    {
    };
    struct ImageObjectExtractorHint : ImageObjectExtractorHintT<ImageObjectExtractorHint, implementation::ImageObjectExtractorHint>
    {
    };
    struct ImageDescriptionGenerator : ImageDescriptionGeneratorT<ImageDescriptionGenerator, implementation::ImageDescriptionGenerator>
    {
    };
}
