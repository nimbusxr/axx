#pragma once
#include "Imaging.TextRecognizer.g.h"
#include "Imaging.RecognizedText.g.h"
#include "Imaging.RecognizedLine.g.h"
#include "Imaging.RecognizedWord.g.h"
#include "Imaging.ImageDescriptionGenerator.g.h"
#include "Imaging.ImageDescriptionResult.g.h"

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

namespace winrt::Microsoft::Windows::AI::Imaging::factory_implementation
{
    struct TextRecognizer : TextRecognizerT<TextRecognizer, implementation::TextRecognizer>
    {
    };
    struct ImageDescriptionGenerator : ImageDescriptionGeneratorT<ImageDescriptionGenerator, implementation::ImageDescriptionGenerator>
    {
    };
}
