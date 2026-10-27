// The Windows AI APIs the WinUI desk uses (../README.md): TextRecognizer reads a parcel's reference
// off the newest scan the depot's label camera saved in Scans; ImageDescriptionGenerator says
// whether a scan shows a parcel, which the desk keeps in Photos with its arrival; and the language
// model, Phi Silica, sums up the day for the next shift. They answer the desk with its package
// identity (identity/).
using System.Text.RegularExpressions;
using Microsoft.Graphics.Imaging;
using Microsoft.Windows.AI;
using Microsoft.Windows.AI.ContentSafety;
using Microsoft.Windows.AI.Imaging;
using Microsoft.Windows.AI.Text;
using Windows.Graphics.Imaging;
using Windows.Storage;

namespace DepotDesk;

/// <summary>The scans the depot's label camera saves in the desk's data folder (Scans), and the
/// photos the desk keeps of its arrivals (Photos).</summary>
static partial class Scans
{
    static string Folder => Path.Combine(Desk.DataFolder, "Scans");
    static string Photos => Path.Combine(Desk.DataFolder, "Photos");

    [GeneratedRegex(@"PX(-[A-Z0-9]+)+")]
    private static partial Regex Reference();

    [GeneratedRegex(@"\b(parcel|package|box)\b", RegexOptions.IgnoreCase)]
    private static partial Regex Parcel();

    /// <summary>The newest scan: the parcel on the counter now.</summary>
    public static string? Newest() =>
        Directory.Exists(Folder)
            ? new DirectoryInfo(Folder).GetFiles()
                .Where(f => f.Extension.ToLowerInvariant() is ".jpg" or ".jpeg" or ".png")
                .OrderBy(f => f.LastWriteTimeUtc).ThenBy(f => f.Name, StringComparer.Ordinal)
                .LastOrDefault()?.FullName
            : null;

    /// <summary>The reference on the newest scan's label, and what the desk says of it.</summary>
    public static async Task<(string? Reference, string Status)> ReadLabelAsync()
    {
        var scan = Newest();
        if (scan == null)
            return (null, "No scan to read");
        try
        {
            if (TextRecognizer.GetReadyState() == AIFeatureReadyState.NotReady)
                await TextRecognizer.EnsureReadyAsync();
            if (TextRecognizer.GetReadyState() != AIFeatureReadyState.Ready)
                return (null, "No label reader on this PC: type the reference in");
            using var recognizer = await TextRecognizer.CreateAsync();
            var text = recognizer.RecognizeTextFromImage(await PictureAsync(scan));
            var found = Reference().Match(string.Join(" ", text.Lines.Select(line => line.Text)));
            return found.Success ? (found.Value, $"Read {found.Value} from the label") : (null, "No reference on the label: type it in");
        }
        catch (UnauthorizedAccessException)
        {
            // A desk without its package identity.
            return (null, "No label reader for this desk: type the reference in");
        }
    }

    /// <summary>Files the newest scan with an arrival: kept in Photos when its description says it
    /// shows a parcel; taken out of Scans either way. What the arrival's status says of it.</summary>
    public static async Task<string> FileAsync(string scan, string reference)
    {
        try
        {
            var seen = await DescribeAsync(scan);
            if (seen == null)
                return ", photo not checked";
            if (!Parcel().IsMatch(seen))
                return ", photo not kept: it shows no parcel";
            Directory.CreateDirectory(Photos);
            File.Copy(scan, Path.Combine(Photos, reference + ".jpg"), true);
            return ", photo kept";
        }
        finally
        {
            File.Delete(scan);
        }
    }

    // The scan's brief description, or null without one.
    static async Task<string?> DescribeAsync(string scan)
    {
        try
        {
            if (ImageDescriptionGenerator.GetReadyState() == AIFeatureReadyState.NotReady)
                await ImageDescriptionGenerator.EnsureReadyAsync();
            if (ImageDescriptionGenerator.GetReadyState() != AIFeatureReadyState.Ready)
                return null;
            using var describer = await ImageDescriptionGenerator.CreateAsync();
            var result = await describer.DescribeAsync(await PictureAsync(scan), ImageDescriptionKind.BriefDescription, new ContentFilterOptions());
            return result.Status == ImageDescriptionResultStatus.Complete ? result.Description : null;
        }
        catch (UnauthorizedAccessException)
        {
            return null;
        }
    }

    static async Task<ImageBuffer> PictureAsync(string path)
    {
        var file = await StorageFile.GetFileFromPathAsync(path);
        using var stream = await file.OpenReadAsync();
        var decoder = await BitmapDecoder.CreateAsync(stream);
        var bitmap = await decoder.GetSoftwareBitmapAsync(BitmapPixelFormat.Bgra8, BitmapAlphaMode.Premultiplied);
        return ImageBuffer.CreateForSoftwareBitmap(bitmap);
    }
}

/// <summary>The day's summary for the next shift: the language model's, Phi Silica's. What the
/// desk says: the summary, or why there is none.</summary>
static class DaySummary
{
    const string Instructions =
        "You sum up a parcel depot's day for the next shift in one short line: how many parcels "
        + "arrived, and which need care.";

    public static async Task<string> WriteAsync(IEnumerable<Arrival> arrivals)
    {
        var prompt = "Today's arrivals at the Leipzig depot: "
            + string.Join("; ", arrivals.Select(a => $"{a.Reference}, {a.Level}" + (a.Fragile ? ", fragile" : ""))) + ".";
        try
        {
            if (LanguageModel.GetReadyState() == AIFeatureReadyState.NotReady)
                await LanguageModel.EnsureReadyAsync();
            switch (LanguageModel.GetReadyState())
            {
                case AIFeatureReadyState.NotSupportedOnCurrentSystem: return "No day summary: this PC has no language model";
                case AIFeatureReadyState.DisabledByUser: return "No day summary: the language model is off";
                case AIFeatureReadyState.NotReady: return "No day summary: the language model is getting ready";
            }
            using var model = await LanguageModel.CreateAsync();
            using var context = model.CreateContext(Instructions);
            var result = await model.GenerateResponseAsync(context, prompt, new LanguageModelOptions());
            var summary = result.Text.Trim();
            return result.Status == LanguageModelResponseStatus.Complete && summary.Length > 0 ? "Day summary: " + summary : "No day summary: try again";
        }
        catch (UnauthorizedAccessException)
        {
            // A desk without its package identity.
            return "No day summary for this desk";
        }
    }
}
