#pragma once
#include "AIFeatureReadyResult.g.h"

namespace winrt::Microsoft::Windows::AI::implementation
{
    // AIFeatureReadyResult is EnsureReadyAsync's result.
    struct AIFeatureReadyResult : AIFeatureReadyResultT<AIFeatureReadyResult>
    {
        AIFeatureReadyResult(AIFeatureReadyResultState status, winrt::hresult error, hstring text)
            : status(status), error(error), text(std::move(text))
        {
        }

        winrt::hresult Error() { return error; }
        hstring ErrorDisplayText() { return text; }
        winrt::hresult ExtendedError() { return error; }
        AIFeatureReadyResultState Status() { return status; }
        bool PackageInstallationFailed() { return false; }

    private:
        AIFeatureReadyResultState status;
        winrt::hresult error;
        hstring text;
    };
}
