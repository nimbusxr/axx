#include "pch.h"
#include "stubs.h"
#include "ready.h"

using namespace winrt;
using namespace winrt::Windows::Data::Json;
using winrt::Microsoft::Graphics::Imaging::ImageBuffer;
using winrt::Microsoft::Graphics::Imaging::ImageBufferPixelFormat;
using winrt::Microsoft::Windows::AI::AIFeatureReadyResult;
using winrt::Microsoft::Windows::AI::AIFeatureReadyResultState;
using winrt::Microsoft::Windows::AI::AIFeatureReadyState;

namespace axx
{
    namespace
    {
        // The APIs of the pictures' classes the library answers, each of its own.
        std::wstring_view ApiOf(std::wstring_view className)
        {
            if (className == L"Microsoft.Windows.AI.Imaging.TextRecognizer")
                return L"windows.ai.text-recognizer";
            if (className == L"Microsoft.Windows.AI.Imaging.ImageDescriptionGenerator")
                return L"windows.ai.image-description";
            if (className == L"Microsoft.Windows.AI.Imaging.ImageObjectExtractor" || className == L"Microsoft.Windows.AI.Imaging.ImageObjectExtractorHint")
                return L"windows.ai.object-extractor";
            return {};
        }

        // The language model's APIs: its own, and its skills'.
        constexpr std::wstring_view textApis[] = { L"windows.ai.language-model", L"windows.ai.text-summarizer", L"windows.ai.text-rewriter", L"windows.ai.text-to-table" };

        bool IsWord(wchar_t c)
        {
            return (c >= L'0' && c <= L'9') || (c >= L'a' && c <= L'z') || (c >= L'A' && c <= L'Z') || c >= 0x80;
        }

        // HasWords tells whether a text has the key, whole words of it: as lm.HasWords does.
        bool HasWords(std::wstring_view text, std::wstring_view key)
        {
            for (size_t i = 0;;)
            {
                size_t at = text.find(key, i);
                if (at == std::wstring_view::npos)
                    return false;
                size_t end = at + key.size();
                if ((at == 0 || !IsWord(text[at - 1]) || !IsWord(key.front())) && (end == text.size() || !IsWord(text[end]) || !IsWord(key.back())))
                    return true;
                i = at + 1;
            }
        }

        std::wstring Environment(wchar_t const* name)
        {
            std::wstring value(32768, L'\0');
            DWORD n = GetEnvironmentVariableW(name, value.data(), static_cast<DWORD>(value.size()));
            value.resize(n < value.size() ? n : 0);
            return value;
        }

        std::wstring Read(std::wstring const& path)
        {
            HANDLE f = CreateFileW(path.c_str(), GENERIC_READ, FILE_SHARE_READ, nullptr, OPEN_EXISTING, FILE_ATTRIBUTE_NORMAL, nullptr);
            if (f == INVALID_HANDLE_VALUE)
                return {};
            std::string bytes;
            char chunk[65536];
            DWORD n = 0;
            while (ReadFile(f, chunk, sizeof chunk, &n, nullptr) && n > 0)
                bytes.append(chunk, n);
            CloseHandle(f);
            int size = MultiByteToWideChar(CP_UTF8, 0, bytes.data(), static_cast<int>(bytes.size()), nullptr, 0);
            std::wstring text(size, L'\0');
            MultiByteToWideChar(CP_UTF8, 0, bytes.data(), static_cast<int>(bytes.size()), text.data(), size);
            return text;
        }

        // The stubs, read once: axx checked them before the app started.
        std::vector<Stub> const& All()
        {
            static std::vector<Stub> const stubs = []
            {
                std::vector<Stub> out;
                std::wstring folder = Environment(L"AXX_MODELS_DIR");
                if (folder.empty())
                    return out;
                WIN32_FIND_DATAW found;
                HANDLE h = FindFirstFileW((folder + L"\\*.axxmodel").c_str(), &found);
                if (h == INVALID_HANDLE_VALUE)
                    return out;
                do
                {
                    Stub s;
                    s.json = JsonObject::Parse(Read(folder + L"\\" + found.cFileName));
                    s.api = s.json.GetNamedString(L"api");
                    s.similarity = s.json.GetNamedNumber(L"similarity", 0);
                    if (s.json.HasKey(L"keys"))
                        for (auto const& key : s.json.GetNamedArray(L"keys"))
                        {
                            std::vector<double> k;
                            for (auto const& v : key.GetArray())
                                k.push_back(v.GetNumber());
                            s.keys.push_back(std::move(k));
                        }
                    out.push_back(std::move(s));
                } while (FindNextFileW(h, &found));
                FindClose(h);
                return out;
            }();
            return stubs;
        }

        // The models a scenario's EnsureReadyAsync made ready, of stubs that were not.
        std::mutex readied;
        std::set<std::wstring> ready;

        void Standardize(double* v, size_t n)
        {
            double mean = 0, variance = 0;
            for (size_t i = 0; i < n; i++)
                mean += v[i];
            mean /= n;
            for (size_t i = 0; i < n; i++)
            {
                v[i] -= mean;
                variance += v[i] * v[i];
            }
            double sd = std::sqrt(variance / n + 1e-12);
            for (size_t i = 0; i < n; i++)
                v[i] /= sd;
        }

        // An ImageBuffer's pixels, whatever its format, as red, green and blue.
        struct Pixels
        {
            std::vector<uint8_t> bytes;
            size_t width = 0, height = 0, stride = 0;
            ImageBufferPixelFormat format{};

            explicit Pixels(ImageBuffer const& image)
                : width(image.PixelWidth()), height(image.PixelHeight()), stride(image.RowStride()), format(image.PixelFormat())
            {
                size_t size = 1;
                switch (format)
                {
                case ImageBufferPixelFormat::Gray8:
                    break;
                case ImageBufferPixelFormat::Rgb8:
                    size = 3;
                    break;
                default:
                    size = 4;
                }
                if (!width || !height || stride < width * size)
                    throw hresult_invalid_argument(L"The image has no pixels");
                bytes.resize(stride * height);
                image.CopyToByteArray(bytes);
            }

            double Channel(size_t x, size_t y, size_t k) const
            {
                uint8_t const* row = bytes.data() + y * stride;
                switch (format)
                {
                case ImageBufferPixelFormat::Gray8:
                    return row[x];
                case ImageBufferPixelFormat::Rgb8:
                    return row[x * 3 + k];
                case ImageBufferPixelFormat::Rgba8:
                    return row[x * 4 + k];
                case ImageBufferPixelFormat::Argb8:
                    return row[x * 4 + 1 + k];
                default: // Bgra8
                    return row[x * 4 + 2 - k];
                }
            }
        };
    }

    Stub const* Find(std::wstring_view api)
    {
        for (auto const& s : All())
            if (s.api == api)
                return &s;
        return nullptr;
    }

    bool Answers(std::wstring_view className)
    {
        if (className.starts_with(L"Microsoft.Windows.AI.Text."))
            return !ModelApi().empty();
        auto api = ApiOf(className);
        return !api.empty() && Find(api);
    }

    std::wstring_view ModelApi()
    {
        for (auto api : textApis)
            if (auto stub = Find(api); stub && (api == textApis[0] || !stub->Availability().empty()))
                return api;
        for (auto api : textApis)
            if (Find(api))
                return api;
        return {};
    }

    std::wstring Respond(std::wstring_view api, std::wstring_view text)
    {
        auto stub = Find(api);
        if (!stub)
            throw hresult_not_implemented(L"axx: the app's axx-models folder has no stub of " + std::wstring(api));
        return stub->Generated(stub->TextIndex(text));
    }

    size_t Stub::TextIndex(std::wstring_view text) const
    {
        auto texts = json.GetNamedArray(L"texts", JsonArray());
        for (uint32_t i = 0; i < texts.Size(); i++)
            if (HasWords(text, texts.GetStringAt(i)))
                return i;
        return texts.Size();
    }

    std::pair<size_t, std::vector<float>> Stub::Mask(size_t at) const
    {
        auto out = json.GetNamedObject(L"contract").GetNamedArray(L"outputs").GetObjectAt(0);
        auto shape = out.GetNamedArray(L"shape");
        auto values = json.GetNamedArray(L"answers").GetObjectAt(static_cast<uint32_t>(at)).GetNamedArray(out.GetNamedString(L"name"));
        std::vector<float> mask(values.Size());
        for (uint32_t i = 0; i < values.Size(); i++)
            mask[i] = static_cast<float>(values.GetNumberAt(i));
        return { static_cast<size_t>(shape.GetNumberAt(shape.Size() - 1)), std::move(mask) };
    }

    std::wstring Stub::Generated(size_t at) const
    {
        return std::wstring(json.GetNamedArray(L"generated").GetStringAt(static_cast<uint32_t>(at)));
    }

    std::wstring Stub::Availability() const
    {
        auto details = json.GetNamedObject(L"contract").GetNamedObject(L"format").GetNamedObject(L"details", nullptr);
        return details ? std::wstring(details.GetNamedString(L"availability", L"")) : std::wstring();
    }

    // The same bilinear crop, pooling and standardization as match.Picture and match.Reducer (and
    // the Apple library's).
    size_t Stub::Picture(ImageBuffer const& image) const
    {
        auto contract = json.GetNamedObject(L"contract");
        std::wstring name;
        if (auto match = contract.GetNamedObject(L"match", nullptr))
            name = match.GetNamedString(L"input", L"");
        JsonObject in{ nullptr };
        for (auto const& v : contract.GetNamedArray(L"inputs"))
            if (auto o = v.GetObject(); name.empty() || o.GetNamedString(L"name") == name)
            {
                in = o;
                break;
            }
        auto spec = in.GetNamedObject(L"image");
        auto shape = in.GetNamedArray(L"shape");
        std::wstring layout{ spec.GetNamedString(L"layout", L"") };
        std::wstring channels{ spec.GetNamedString(L"channels", L"") };
        bool first = layout.size() >= 3 && layout.compare(layout.size() - 3, 3, L"CHW") == 0;
        uint32_t off = shape.Size() - 3;
        auto dim = [&](uint32_t i) { return static_cast<size_t>(shape.GetNumberAt(off + i)); };
        size_t c = dim(first ? 0 : 2), h = dim(first ? 1 : 0), w = dim(first ? 2 : 1);

        Pixels px(image);
        size_t sw = px.width, sh = px.height, cw = sw, ch = sh, ox = 0, oy = 0;
        if (spec.GetNamedString(L"resize", L"") != L"stretch")
        {
            if (sw * h > sh * w)
            {
                cw = sh * w / h;
                ox = (sw - cw) / 2;
            }
            else
            {
                ch = sw * h / w;
                oy = (sh - ch) / 2;
            }
        }
        double crop = spec.GetNamedNumber(L"crop", 0);
        if (crop > 0 && crop < 1)
        {
            size_t nw = std::llround(cw * crop), nh = std::llround(ch * crop);
            ox += (cw - nw) / 2;
            oy += (ch - nh) / 2;
            cw = nw;
            ch = nh;
        }
        if (!cw || !ch)
            throw hresult_invalid_argument(L"The image crop is empty");

        size_t kh = std::max<size_t>(h / std::min<size_t>(16, h), 1), kw = std::max<size_t>(w / std::min<size_t>(16, w), 1);
        size_t ph = h / kh, pw = w / kw, n = c * ph * pw;
        std::vector<double> v(n);
        for (size_t y = 0; y < ph * kh; y++)
            for (size_t x = 0; x < pw * kw; x++)
            {
                double sx = (x + 0.5) * cw / w - 0.5, sy = (y + 0.5) * ch / h - 0.5;
                double fx = sx - std::floor(sx), fy = sy - std::floor(sy), rgb[3];
                int64_t x0 = static_cast<int64_t>(std::floor(sx)), y0 = static_cast<int64_t>(std::floor(sy));
                for (size_t k = 0; k < 3; k++)
                {
                    double samples[4];
                    for (int dy = 0; dy < 2; dy++)
                        for (int dx = 0; dx < 2; dx++)
                        {
                            size_t qx = ox + static_cast<size_t>(std::min<int64_t>(std::max<int64_t>(x0 + dx, 0), static_cast<int64_t>(cw) - 1));
                            size_t qy = oy + static_cast<size_t>(std::min<int64_t>(std::max<int64_t>(y0 + dy, 0), static_cast<int64_t>(ch) - 1));
                            samples[dy * 2 + dx] = px.Channel(qx, qy, k);
                        }
                    double top = samples[0] * (1 - fx) + samples[1] * fx, bottom = samples[2] * (1 - fx) + samples[3] * fx;
                    rgb[k] = top * (1 - fy) + bottom * fy;
                }
                for (size_t k = 0; k < c; k++)
                {
                    double value = k < 3 ? rgb[channels == L"BGR" ? 2 - k : k] : 255;
                    if (c == 1 || channels == L"gray")
                        value = 0.299 * rgb[0] + 0.587 * rgb[1] + 0.114 * rgb[2];
                    v[k * ph * pw + (y / kh) * pw + x / kw] += value;
                }
            }
        for (auto& value : v)
            value /= static_cast<double>(kh * kw);
        for (size_t k = 0; k < c; k++)
            Standardize(v.data() + k * ph * pw, ph * pw);

        size_t at = keys.size();
        double best = similarity;
        for (size_t i = keys.size(); i-- > 0;)
        {
            if (keys[i].size() != n)
                continue;
            double dot = 0;
            for (size_t j = 0; j < n; j++)
                dot += v[j] * keys[i][j];
            if (dot / n >= best)
            {
                best = dot / n;
                at = i;
            }
        }
        return at;
    }

    std::pair<std::wstring, float> Stub::Text(size_t at) const
    {
        auto out = json.GetNamedObject(L"contract").GetNamedArray(L"outputs").GetObjectAt(0);
        auto labels = out.GetNamedArray(L"labels");
        auto values = json.GetNamedArray(L"answers").GetObjectAt(static_cast<uint32_t>(at)).GetNamedArray(out.GetNamedString(L"name"));
        uint32_t k = labels.Size();
        std::wstring text;
        float score = 1;
        int64_t last = -1;
        for (uint32_t s = 0; s < values.Size() / k; s++)
        {
            uint32_t best = 0;
            for (uint32_t j = 1; j < k; j++)
                if (values.GetNumberAt(s * k + j) > values.GetNumberAt(s * k + best))
                    best = j;
            if (static_cast<int64_t>(best) != last && !labels.GetStringAt(best).empty())
            {
                text += labels.GetStringAt(best);
                score = std::min(score, static_cast<float>(values.GetNumberAt(s * k + best)));
            }
            last = best;
        }
        return { text, score };
    }

    bool Allowed()
    {
        static bool const allowed = []
        {
            UINT32 n = 0;
            if (GetCurrentPackageFullName(&n, nullptr) == APPMODEL_ERROR_NO_PACKAGE)
                return false;
            try
            {
                using namespace winrt::Windows::Security::Authorization::AppCapabilityAccess;
                return AppCapability::Create(L"systemAIModels").CheckAccess() == AppCapabilityAccessStatus::Allowed;
            }
            catch (...)
            {
                return false;
            }
        }();
        return allowed;
    }

    AIFeatureReadyState ReadyState(std::wstring_view api)
    {
        if (!Allowed())
            throw hresult_access_denied(L"Denied access");
        auto stub = Find(api);
        std::wstring why = stub ? stub->Availability() : L"";
        if (why == L"NotSupportedOnCurrentSystem")
            return AIFeatureReadyState::NotSupportedOnCurrentSystem;
        if (why == L"NotReady")
        {
            std::lock_guard lock(readied);
            return ready.count(std::wstring(api)) ? AIFeatureReadyState::Ready : AIFeatureReadyState::NotReady;
        }
        return AIFeatureReadyState::Ready;
    }

    winrt::Windows::Foundation::IAsyncOperationWithProgress<AIFeatureReadyResult, double> EnsureReady(std::wstring api)
    {
        auto progress = co_await get_progress_token();
        co_await resume_background();
        if (!Allowed())
            co_return make<winrt::Microsoft::Windows::AI::implementation::AIFeatureReadyResult>(AIFeatureReadyResultState::Failure, E_ACCESSDENIED, L"Capability requirements not met");
        auto stub = Find(api);
        std::wstring why = stub ? stub->Availability() : L"";
        if (why == L"NotSupportedOnCurrentSystem")
            co_return make<winrt::Microsoft::Windows::AI::implementation::AIFeatureReadyResult>(AIFeatureReadyResultState::Failure, HRESULT_FROM_WIN32(ERROR_NOT_FOUND), L"");
        if (why == L"NotReady")
        {
            std::lock_guard lock(readied);
            ready.insert(api);
        }
        progress(1.0);
        co_return make<winrt::Microsoft::Windows::AI::implementation::AIFeatureReadyResult>(AIFeatureReadyResultState::Success, S_OK, L"");
    }

    void Creatable(std::wstring_view api)
    {
        switch (ReadyState(api))
        {
        case AIFeatureReadyState::NotSupportedOnCurrentSystem:
            throw hresult_error(HRESULT_FROM_WIN32(ERROR_NOT_FOUND));
        case AIFeatureReadyState::NotReady:
            throw hresult_access_denied();
        default:
            return;
        }
    }
}
