// axx's library of the Windows AI APIs (models-windows): the Windows App SDK's classes a
// self-contained app's manifest maps to its Microsoft.Windows.AI.*.dll files, which this library
// takes the place of in the app's test runs, answered from the stubs in the app's axx-models
// folder (AXX_MODELS_DIR). C++/WinRT's projection and the classes' bases are generated from the
// app's own .winmd files as axx builds it.
#pragma once
#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#include <windows.h>
#include <unknwn.h>
#include <appmodel.h>
#include <winstring.h>
#include <winrt/base.h>
#include <winrt/Windows.Foundation.h>
#include <winrt/Windows.Foundation.Collections.h>
#include <winrt/Windows.Data.Json.h>
#include <winrt/Windows.Storage.Streams.h>
#include <winrt/Windows.Security.Authorization.AppCapabilityAccess.h>
#include <winrt/Microsoft.Graphics.Imaging.h>
#include <winrt/Microsoft.Windows.AI.h>
#include <winrt/Microsoft.Windows.AI.Imaging.h>
#include <algorithm>
#include <cmath>
#include <mutex>
#include <set>
#include <string>
#include <utility>
#include <vector>
