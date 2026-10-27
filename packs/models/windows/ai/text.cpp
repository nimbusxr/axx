#include "pch.h"
#include "text.h"
#include "imaging.h"
#include "stubs.h"
#include "Text.ConversationItem.g.cpp"
#include "Text.ConversationSummaryOptions.g.cpp"
#include "Text.LanguageModel.g.cpp"
#include "Text.LanguageModelContext.g.cpp"
#include "Text.LanguageModelOptions.g.cpp"
#include "Text.LanguageModelResponseResult.g.cpp"
#include "Text.TextRewriter.g.cpp"
#include "Text.TextSummarizer.g.cpp"
#include "Text.TextToTableConverter.g.cpp"
#include "Text.TextToTableResponseResult.g.cpp"
#include "Text.TextToTableRow.g.cpp"

using namespace winrt;
using namespace winrt::Windows::Foundation;
using namespace winrt::Windows::Foundation::Collections;
using winrt::Microsoft::Windows::AI::Foundation::EmbeddingVector;

namespace winrt::Microsoft::Windows::AI::Text::implementation
{
    namespace
    {
        constexpr wchar_t model[] = L"windows.ai.language-model";

        // Answer answers a text API, its answer streamed a word at a time, as the model's
        // progress gives it.
        Response Answer(std::wstring api, std::wstring text)
        {
            auto progress = co_await get_progress_token();
            co_await resume_background();
            std::wstring answer = axx::Respond(api, text);
            co_await axx::Stream(progress, answer);
            co_return make<LanguageModelResponseResult>(hstring(answer));
        }

        std::wstring Conversation(Text::LanguageModelContext const& context, hstring const& prompt)
        {
            std::wstring text = context ? std::wstring(get_self<LanguageModelContext>(context)->systemPrompt) : L"";
            return text.empty() ? std::wstring(prompt) : text + L"\n" + std::wstring(prompt);
        }

        [[noreturn]] void NoEmbeddings()
        {
            throw hresult_not_implemented(L"axx: a stub of the language model answers prompts, not embedding vectors");
        }
    }

    AIFeatureReadyState LanguageModel::GetReadyState()
    {
        return axx::ReadyState(axx::ModelApi());
    }

    IAsyncOperationWithProgress<AIFeatureReadyResult, double> LanguageModel::EnsureReadyAsync()
    {
        return axx::EnsureReady(std::wstring(axx::ModelApi()));
    }

    IAsyncOperation<Text::LanguageModel> LanguageModel::CreateAsync()
    {
        axx::Creatable(axx::ModelApi());
        co_return make<LanguageModel>();
    }

    Response LanguageModel::GenerateResponseAsync(hstring prompt)
    {
        return Answer(model, std::wstring(prompt));
    }

    Response LanguageModel::GenerateResponseAsync(hstring prompt, Text::LanguageModelOptions)
    {
        return Answer(model, std::wstring(prompt));
    }

    Response LanguageModel::GenerateResponseAsync(Text::LanguageModelContext context, hstring prompt, Text::LanguageModelOptions)
    {
        return Answer(model, Conversation(context, prompt));
    }

    Response LanguageModel::GenerateResponseFromEmbeddingsAsync(IIterable<EmbeddingVector>) { NoEmbeddings(); }
    Response LanguageModel::GenerateResponseFromEmbeddingsAsync(IIterable<EmbeddingVector>, Text::LanguageModelOptions) { NoEmbeddings(); }
    Response LanguageModel::GenerateResponseFromEmbeddingsAsync(Text::LanguageModelContext, IIterable<EmbeddingVector>, Text::LanguageModelOptions) { NoEmbeddings(); }
    Text::LanguageModelEmbeddingVectorResult LanguageModel::GenerateEmbeddingVectors(hstring const&) { NoEmbeddings(); }
    Text::LanguageModelEmbeddingVectorResult LanguageModel::GenerateEmbeddingVectors(hstring const&, winrt::Microsoft::Windows::AI::ContentSafety::ContentFilterOptions const&) { NoEmbeddings(); }

    uint64_t LanguageModel::GetUsablePromptLength(Text::LanguageModelContext const& context, hstring const& prompt)
    {
        return Conversation(context, prompt).size();
    }

    winrt::guid LanguageModel::GetVectorSpaceId()
    {
        return winrt::guid{ 0x6178786d, 0x6f64, 0x656c, { 0x73, 0x2d, 0x77, 0x69, 0x6e, 0x64, 0x6f, 0x77 } };
    }

    Text::LanguageModelContext LanguageModel::CreateContext()
    {
        return make<LanguageModelContext>(hstring());
    }

    Text::LanguageModelContext LanguageModel::CreateContext(hstring const& systemPrompt)
    {
        return make<LanguageModelContext>(systemPrompt);
    }

    Text::LanguageModelContext LanguageModel::CreateContext(hstring const& systemPrompt, winrt::Microsoft::Windows::AI::ContentSafety::ContentFilterOptions const&)
    {
        return make<LanguageModelContext>(systemPrompt);
    }

    Response TextSummarizer::SummarizeAsync(hstring text)
    {
        return Answer(L"windows.ai.text-summarizer", std::wstring(text));
    }

    Response TextSummarizer::SummarizeParagraphAsync(hstring text)
    {
        return Answer(L"windows.ai.text-summarizer", std::wstring(text));
    }

    // A conversation is its messages, each a line of its participant's.
    Response TextSummarizer::SummarizeConversationAsync(IVectorView<Text::ConversationItem> messages, Text::ConversationSummaryOptions)
    {
        std::wstring text;
        for (auto const& m : messages)
            text += std::wstring(m.Participant()) + L": " + std::wstring(m.Message()) + L"\n";
        return Answer(L"windows.ai.text-summarizer", text);
    }

    bool TextSummarizer::IsPromptLargerThanContext(array_view<Text::ConversationItem const> messages, Text::ConversationSummaryOptions const&, uint64_t& cutoffPosition)
    {
        cutoffPosition = messages.size();
        return false;
    }

    bool TextSummarizer::IsPromptLargerThanContext(hstring const& text, uint64_t& cutoffPosition)
    {
        cutoffPosition = text.size();
        return false;
    }

    Response TextRewriter::RewriteAsync(hstring text)
    {
        return Answer(L"windows.ai.text-rewriter", std::wstring(text));
    }

    Response TextRewriter::RewriteAsync(hstring text, TextRewriteTone)
    {
        return Answer(L"windows.ai.text-rewriter", std::wstring(text));
    }

    Response TextRewriter::RewriteCustomAsync(hstring text, hstring customTone)
    {
        return Answer(L"windows.ai.text-rewriter", std::wstring(text) + L"\n" + std::wstring(customTone));
    }

    // A table is its answer's JSON: an array of rows, each an array of its columns; an answer of
    // text is a row of one column.
    IAsyncOperationWithProgress<Text::TextToTableResponseResult, hstring> TextToTableConverter::ConvertAsync(hstring text)
    {
        auto progress = co_await get_progress_token();
        co_await resume_background();
        std::wstring answer = axx::Respond(L"windows.ai.text-to-table", text);
        std::vector<Text::TextToTableRow> rows;
        winrt::Windows::Data::Json::JsonArray table{ nullptr };
        if (winrt::Windows::Data::Json::JsonArray::TryParse(answer, table))
            for (auto const& row : table)
            {
                std::vector<hstring> columns;
                for (auto const& column : row.GetArray())
                    columns.push_back(column.GetString());
                rows.push_back(make<TextToTableRow>(std::move(columns)));
            }
        else if (!answer.empty())
            rows.push_back(make<TextToTableRow>(std::vector<hstring>{ hstring(answer) }));
        co_await axx::Stream(progress, answer);
        co_return make<TextToTableResponseResult>(std::move(rows));
    }

    com_array<Text::TextToTableRow> TextToTableResponseResult::GetRows()
    {
        return axx::Array(rows);
    }

    com_array<hstring> TextToTableRow::GetColumns()
    {
        return axx::Array(columns);
    }
}
