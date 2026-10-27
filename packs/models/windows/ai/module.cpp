// The library's entry, DllGetActivationFactory, for the classes the app's manifest maps to the
// file the library takes the place of (one of the app's Microsoft.Windows.AI.*.dll): a class
// whose API the app's axx-models folder has a stub of is the library's, and any other the app's
// own file's, which models-windows keeps in AXX_WINDOWS_AI_ORIGINALS for the scenario.
#include "pch.h"
#include "stubs.h"

int32_t __stdcall WINRT_GetActivationFactory(void* classId, void** factory) noexcept;

// The library's mark, an export of its own, by which models-windows knows its file in an app's
// folder.
extern "C" char const AxxModelsWindows[] = "axx: models-windows' library of the Windows AI APIs";

namespace
{
    using GetFactory = HRESULT(__stdcall*)(HSTRING, void**);

    std::wstring ModuleName()
    {
        HMODULE self{};
        GetModuleHandleExW(GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS | GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT, reinterpret_cast<LPCWSTR>(&ModuleName), &self);
        std::wstring path(32768, L'\0');
        path.resize(GetModuleFileNameW(self, path.data(), static_cast<DWORD>(path.size())));
        return path.substr(path.find_last_of(L"\\/") + 1);
    }

    // Original is the original file's DllGetActivationFactory, or why it cannot be loaded.
    std::pair<GetFactory, HRESULT> Original()
    {
        static std::pair<GetFactory, HRESULT> const get = []() -> std::pair<GetFactory, HRESULT>
        {
            std::wstring dir(32768, L'\0');
            DWORD n = GetEnvironmentVariableW(L"AXX_WINDOWS_AI_ORIGINALS", dir.data(), static_cast<DWORD>(dir.size()));
            if (n == 0 || n >= dir.size())
                return { nullptr, CLASS_E_CLASSNOTAVAILABLE };
            dir.resize(n);
            // Its own folder, then the app's (where its dependencies are) and the system's.
            HMODULE original = LoadLibraryExW((dir + L"\\" + ModuleName()).c_str(), nullptr, LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR | LOAD_LIBRARY_SEARCH_DEFAULT_DIRS);
            if (!original)
                return { nullptr, HRESULT_FROM_WIN32(GetLastError()) };
            // A library of axx's is not the app's file, whatever its name.
            if (GetProcAddress(original, "AxxModelsWindows"))
                return { nullptr, CLASS_E_CLASSNOTAVAILABLE };
            auto get = reinterpret_cast<GetFactory>(GetProcAddress(original, "DllGetActivationFactory"));
            return { get, get ? S_OK : HRESULT_FROM_WIN32(GetLastError()) };
        }();
        return get;
    }
}

extern "C" HRESULT __stdcall DllGetActivationFactory(HSTRING classId, void** factory) try
{
    UINT32 length = 0;
    wchar_t const* name = WindowsGetStringRawBuffer(classId, &length);
    if (axx::Answers({ name, length }))
        return WINRT_GetActivationFactory(classId, factory);
    auto [get, why] = Original();
    if (get)
        return get(classId, factory);
    *factory = nullptr;
    return why;
}
catch (...)
{
    *factory = nullptr;
    return winrt::to_hresult();
}

// The library stays for as long as the app runs, as the original's classes it hands on may.
STDAPI DllCanUnloadNow()
{
    return S_FALSE;
}
