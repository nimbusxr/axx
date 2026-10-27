// How the WinUI desk reads a parcel's label (../README.md): the newest scan the depot's label camera
// saved in Scans, with the Windows AI APIs' TextRecognizer, which answers the desk with its package
// identity (identity/).
using System.Text.RegularExpressions;
using Microsoft.Graphics.Imaging;
using Microsoft.Windows.AI;
using Microsoft.Windows.AI.Imaging;
using Windows.Graphics.Imaging;
using Windows.Storage;

namespace DepotDesk;

static partial class Labels
{
    [GeneratedRegex(@"PX(-[A-Z0-9]+)+")]
    private static partial Regex Reference();

    /// <summary>The reference on the newest scan's label, and what the desk says of it.</summary>
    public static async Task<(string? Reference, string Status)> ReadAsync(string folder)
    {
        var scan = Newest(folder);
        if (scan == null)
            return (null, "No scan to read");
        try
        {
            if (TextRecognizer.GetReadyState() == AIFeatureReadyState.NotReady)
                await TextRecognizer.EnsureReadyAsync();
            if (TextRecognizer.GetReadyState() != AIFeatureReadyState.Ready)
                return (null, "No label reader on this PC: type the reference in");
            var file = await StorageFile.GetFileFromPathAsync(scan);
            using var stream = await file.OpenReadAsync();
            var decoder = await BitmapDecoder.CreateAsync(stream);
            using var bitmap = await decoder.GetSoftwareBitmapAsync(BitmapPixelFormat.Bgra8, BitmapAlphaMode.Premultiplied);
            using var recognizer = await TextRecognizer.CreateAsync();
            var text = recognizer.RecognizeTextFromImage(ImageBuffer.CreateForSoftwareBitmap(bitmap));
            var found = Reference().Match(string.Join(" ", text.Lines.Select(line => line.Text)));
            return found.Success ? (found.Value, $"Read {found.Value} from the label") : (null, "No reference on the label: type it in");
        }
        catch (UnauthorizedAccessException)
        {
            // A desk without its package identity.
            return (null, "No label reader for this desk: type the reference in");
        }
    }

    static string? Newest(string folder) =>
        Directory.Exists(folder)
            ? new DirectoryInfo(folder).GetFiles()
                .Where(f => f.Extension.ToLowerInvariant() is ".jpg" or ".jpeg" or ".png")
                .OrderBy(f => f.LastWriteTimeUtc).ThenBy(f => f.Name, StringComparer.Ordinal)
                .LastOrDefault()?.FullName
            : null;
}
