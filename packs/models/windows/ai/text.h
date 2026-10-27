#pragma once
#include "Text.ConversationItem.g.h"
#include "Text.ConversationSummaryOptions.g.h"
#include "Text.LanguageModel.g.h"
#include "Text.LanguageModelContext.g.h"
#include "Text.LanguageModelOptions.g.h"
#include "Text.LanguageModelResponseResult.g.h"
#include "Text.TextRewriter.g.h"
#include "Text.TextSummarizer.g.h"
#include "Text.TextToTableConverter.g.h"
#include "Text.TextToTableResponseResult.g.h"
#include "Text.TextToTableRow.g.h"

// The language model, Phi Silica, and its skills: each answers the text the app gives it from its
// API's stub, which its mappings' keys are in.
namespace winrt::Microsoft::Windows::AI::Text::implementation
{
    using Response = winrt::Windows::Foundation::IAsyncOperationWithProgress<Text::LanguageModelResponseResult, hstring>;

    struct LanguageModel : LanguageModelT<LanguageModel>
    {
        LanguageModel() = default;

        static AIFeatureReadyState GetReadyState();
        static winrt::Windows::Foundation::IAsyncOperationWithProgress<AIFeatureReadyResult, double> EnsureReadyAsync();
        static winrt::Windows::Foundation::IAsyncOperation<Text::LanguageModel> CreateAsync();
        Response GenerateResponseAsync(hstring prompt);
        Response GenerateResponseAsync(hstring prompt, Text::LanguageModelOptions options);
        Response GenerateResponseAsync(Text::LanguageModelContext context, hstring prompt, Text::LanguageModelOptions options);
        Response GenerateResponseFromEmbeddingsAsync(winrt::Windows::Foundation::Collections::IIterable<winrt::Microsoft::Windows::AI::Foundation::EmbeddingVector> promptEmbedding);
        Response GenerateResponseFromEmbeddingsAsync(winrt::Windows::Foundation::Collections::IIterable<winrt::Microsoft::Windows::AI::Foundation::EmbeddingVector> promptEmbedding, Text::LanguageModelOptions options);
        Response GenerateResponseFromEmbeddingsAsync(Text::LanguageModelContext context, winrt::Windows::Foundation::Collections::IIterable<winrt::Microsoft::Windows::AI::Foundation::EmbeddingVector> promptEmbedding, Text::LanguageModelOptions options);
        Text::LanguageModelEmbeddingVectorResult GenerateEmbeddingVectors(hstring const& prompt);
        Text::LanguageModelEmbeddingVectorResult GenerateEmbeddingVectors(hstring const& prompt, winrt::Microsoft::Windows::AI::ContentSafety::ContentFilterOptions const& contentFilterOptions);
        uint64_t GetUsablePromptLength(hstring const& prompt) { return prompt.size(); }
        uint64_t GetUsablePromptLength(Text::LanguageModelContext const& context, hstring const& prompt);
        winrt::guid GetVectorSpaceId();
        Text::LanguageModelContext CreateContext();
        Text::LanguageModelContext CreateContext(hstring const& systemPrompt);
        Text::LanguageModelContext CreateContext(hstring const& systemPrompt, winrt::Microsoft::Windows::AI::ContentSafety::ContentFilterOptions const& contentFilterOptions);
        void Close() {}
    };

    // A context is its system prompt, which a prompt's conversation begins with.
    struct LanguageModelContext : LanguageModelContextT<LanguageModelContext>
    {
        explicit LanguageModelContext(hstring systemPrompt) : systemPrompt(std::move(systemPrompt)) {}
        void Close() {}
        hstring systemPrompt;
    };

    struct LanguageModelResponseResult : LanguageModelResponseResultT<LanguageModelResponseResult>
    {
        explicit LanguageModelResponseResult(hstring text) : text(std::move(text)) {}
        hstring Text() { return text; }
        LanguageModelResponseStatus Status() { return LanguageModelResponseStatus::Complete; }
        winrt::hresult ExtendedError() { return S_OK; }

    private:
        hstring text;
    };

    struct LanguageModelOptions : LanguageModelOptionsT<LanguageModelOptions>
    {
        LanguageModelOptions() = default;
        float Temperature() { return temperature; }
        void Temperature(float value) { temperature = value; }
        float TopP() { return topP; }
        void TopP(float value) { topP = value; }
        uint32_t TopK() { return topK; }
        void TopK(uint32_t value) { topK = value; }
        winrt::Microsoft::Windows::AI::ContentSafety::ContentFilterOptions ContentFilterOptions() { return filter; }
        void ContentFilterOptions(winrt::Microsoft::Windows::AI::ContentSafety::ContentFilterOptions const& value) { filter = value; }

    private:
        float temperature = 0, topP = 0;
        uint32_t topK = 0;
        winrt::Microsoft::Windows::AI::ContentSafety::ContentFilterOptions filter{ nullptr };
    };

    struct ConversationItem : ConversationItemT<ConversationItem>
    {
        ConversationItem() = default;
        hstring Participant() { return participant; }
        void Participant(hstring const& value) { participant = value; }
        hstring Message() { return message; }
        void Message(hstring const& value) { message = value; }

    private:
        hstring participant, message;
    };

    struct ConversationSummaryOptions : ConversationSummaryOptionsT<ConversationSummaryOptions>
    {
        ConversationSummaryOptions() = default;
        Text::InputKind InputKind() { return kind; }
        void InputKind(Text::InputKind const& value) { kind = value; }
        bool IncludeMessageCitations() { return citations; }
        void IncludeMessageCitations(bool value) { citations = value; }
        bool IncludeParticipantAttribution() { return attribution; }
        void IncludeParticipantAttribution(bool value) { attribution = value; }
        uint32_t MaxKeyPoints() { return keyPoints; }
        void MaxKeyPoints(uint32_t value) { keyPoints = value; }
        hstring Language() { return language; }
        void Language(hstring const& value) { language = value; }

    private:
        Text::InputKind kind = Text::InputKind::GeneralConversation;
        bool citations = false, attribution = false;
        uint32_t keyPoints = 0;
        hstring language;
    };

    struct TextSummarizer : TextSummarizerT<TextSummarizer>
    {
        explicit TextSummarizer(Text::LanguageModel const&) {}
        Response SummarizeAsync(hstring text);
        Response SummarizeParagraphAsync(hstring text);
        Response SummarizeConversationAsync(winrt::Windows::Foundation::Collections::IVectorView<Text::ConversationItem> messages, Text::ConversationSummaryOptions options);
        bool IsPromptLargerThanContext(array_view<Text::ConversationItem const> messages, Text::ConversationSummaryOptions const& options, uint64_t& cutoffPosition);
        bool IsPromptLargerThanContext(hstring const& text, uint64_t& cutoffPosition);
    };

    struct TextRewriter : TextRewriterT<TextRewriter>
    {
        explicit TextRewriter(Text::LanguageModel const&) {}
        Response RewriteAsync(hstring text);
        Response RewriteAsync(hstring text, TextRewriteTone tone);
        Response RewriteCustomAsync(hstring text, hstring customTone);
    };

    struct TextToTableConverter : TextToTableConverterT<TextToTableConverter>
    {
        explicit TextToTableConverter(Text::LanguageModel const&) {}
        winrt::Windows::Foundation::IAsyncOperationWithProgress<Text::TextToTableResponseResult, hstring> ConvertAsync(hstring text);
    };

    struct TextToTableResponseResult : TextToTableResponseResultT<TextToTableResponseResult>
    {
        explicit TextToTableResponseResult(std::vector<Text::TextToTableRow> rows) : rows(std::move(rows)) {}
        com_array<Text::TextToTableRow> GetRows();
        LanguageModelResponseStatus Status() { return LanguageModelResponseStatus::Complete; }
        winrt::hresult ExtendedError() { return S_OK; }

    private:
        std::vector<Text::TextToTableRow> rows;
    };

    struct TextToTableRow : TextToTableRowT<TextToTableRow>
    {
        explicit TextToTableRow(std::vector<hstring> columns) : columns(std::move(columns)) {}
        com_array<hstring> GetColumns();

    private:
        std::vector<hstring> columns;
    };
}

namespace winrt::Microsoft::Windows::AI::Text::factory_implementation
{
    struct LanguageModel : LanguageModelT<LanguageModel, implementation::LanguageModel> {};
    struct LanguageModelOptions : LanguageModelOptionsT<LanguageModelOptions, implementation::LanguageModelOptions> {};
    struct ConversationItem : ConversationItemT<ConversationItem, implementation::ConversationItem> {};
    struct ConversationSummaryOptions : ConversationSummaryOptionsT<ConversationSummaryOptions, implementation::ConversationSummaryOptions> {};
    struct TextSummarizer : TextSummarizerT<TextSummarizer, implementation::TextSummarizer> {};
    struct TextRewriter : TextRewriterT<TextRewriter, implementation::TextRewriter> {};
    struct TextToTableConverter : TextToTableConverterT<TextToTableConverter, implementation::TextToTableConverter> {};
}
