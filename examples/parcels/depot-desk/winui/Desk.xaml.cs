// The depot desk in WinUI 3 (../README.md).
using System.Text.Json;
using Microsoft.UI;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Automation.Peers;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Input;
using Microsoft.UI.Xaml.Media;
using Microsoft.UI.Xaml.Media.Imaging;
using Microsoft.UI.Xaml.Shapes;
using Microsoft.Win32;
using Windows.Foundation;
using Windows.System;

namespace DepotDesk;

public record Arrival(string Reference, string Level, bool Fragile);

/// <summary>
/// Where the courier signs: a click puts a dot, a drag draws a stroke. A panel has no
/// automation peer of its own, so the pad gives one: an image named Courier signature.
/// </summary>
public partial class SignaturePad : Grid
{
    Polyline? stroke;
    public event Action? Signed;

    public SignaturePad()
    {
        Background = new SolidColorBrush(Colors.White);
        PointerPressed += (_, e) =>
        {
            CapturePointer(e.Pointer);
            var p = e.GetCurrentPoint(this).Position;
            stroke = new Polyline { Stroke = new SolidColorBrush(Colors.Black), StrokeThickness = 3, StrokeStartLineCap = PenLineCap.Round, StrokeEndLineCap = PenLineCap.Round };
            stroke.Points.Add(p);
            stroke.Points.Add(new Point(p.X + 0.5, p.Y + 0.5));
            Children.Add(stroke);
            Signed?.Invoke();
        };
        PointerMoved += (_, e) => stroke?.Points.Add(e.GetCurrentPoint(this).Position);
        PointerReleased += (_, e) => { stroke = null; ReleasePointerCapture(e.Pointer); };
    }

    public void Clear() => Children.Clear();

    protected override AutomationPeer OnCreateAutomationPeer() => new Peer(this);

    sealed partial class Peer(SignaturePad pad) : FrameworkElementAutomationPeer(pad)
    {
        protected override AutomationControlType GetAutomationControlTypeCore() => AutomationControlType.Image;
        protected override string GetNameCore() => "Courier signature";
        protected override string GetClassNameCore() => nameof(SignaturePad);
    }
}

public sealed partial class Desk : Window
{
    static readonly string[] Towns = ["Leipzig", "Halle", "Markkleeberg", "Taucha", "Schkeuditz", "Delitzsch", "Borna", "Grimma", "Wurzen", "Eilenburg"];
    const string SettingsKey = @"Software\Parcels\Depot desk";
    static string DataFolder => System.IO.Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData), "Parcels", "Depot desk");

    List<Arrival> arrivals = Load();

    static string Parcels(int n) => n == 1 ? "1 parcel" : $"{n} parcels";

    [System.Runtime.InteropServices.DllImport("user32.dll")]
    static extern uint GetDpiForWindow(IntPtr hwnd);

    public Desk()
    {
        InitializeComponent();
        // The window's size in points: AppWindow takes pixels.
        var scale = GetDpiForWindow(WinRT.Interop.WindowNative.GetWindowHandle(this)) / 96.0;
        AppWindow.Resize(new Windows.Graphics.SizeInt32((int)(640 * scale), (int)(580 * scale)));
        Logo.Source = new BitmapImage(new Uri(System.IO.Path.Combine(AppContext.BaseDirectory, "depot.png")));
        (LevelKept() == "Express" ? Express : Standard).IsChecked = true;
        // Each row built here, not by a typed template: WinUI's XAML compiler fails on
        // one (WMC9999) when built with dotnet build.
        for (var i = 0; i < 40; i++)
        {
            var reff = $"PX-DSK-{4101 + i}";
            var row = new Grid { Tag = reff };
            row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(170) });
            row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(170) });
            var town = new TextBlock { Text = Towns[i % Towns.Length] };
            Grid.SetColumn(town, 1);
            row.Children.Add(new TextBlock { Text = reff });
            row.Children.Add(town);
            var item = new ListViewItem { Content = row };
            Microsoft.UI.Xaml.Automation.AutomationProperties.SetName(item, reff);
            Expected.Items.Add(item);
        }
        Pad.Signed += () => Signed.Text = "Signed";
        Refresh();
        Status.Text = arrivals.Count == 0 ? "No parcels registered yet" : $"{Parcels(arrivals.Count)} registered today";
    }

    static List<Arrival> Load()
    {
        try { return JsonSerializer.Deserialize<List<Arrival>>(System.IO.File.ReadAllText(System.IO.Path.Combine(DataFolder, "arrivals.json"))) ?? []; }
        catch (System.IO.IOException) { return []; }
    }

    static void Save(List<Arrival> arrivals)
    {
        System.IO.Directory.CreateDirectory(DataFolder);
        System.IO.File.WriteAllText(System.IO.Path.Combine(DataFolder, "arrivals.json"), JsonSerializer.Serialize(arrivals));
    }

    static string LevelKept() => Registry.CurrentUser.OpenSubKey(SettingsKey)?.GetValue("serviceLevel") as string ?? "Standard";

    void Refresh()
    {
        RegisterButton.IsEnabled = Reference.Text.Trim().Length > 0;
        CloseDayItem.IsEnabled = arrivals.Count > 0;
        ArrivalsList.ItemsSource = arrivals.Select(a => a.Reference).ToList();
    }

    void OnReferenceChanged(object sender, TextChangedEventArgs e) => Refresh();

    void OnReferenceKey(object sender, KeyRoutedEventArgs e)
    {
        if (e.Key == VirtualKey.Enter) { Register(); e.Handled = true; }
        if (e.Key == VirtualKey.Escape) { Reference.Text = ""; e.Handled = true; }
    }

    void OnRegister(object sender, RoutedEventArgs e) => Register();

    void Register()
    {
        var reff = Reference.Text.Trim();
        if (reff.Length == 0) return;
        if (arrivals.Any(a => a.Reference == reff))
        {
            Status.Text = $"{reff} is already registered";
            return;
        }
        var level = Express.IsChecked == true ? "Express" : "Standard";
        var fragile = Fragile.IsChecked == true;
        arrivals.Add(new Arrival(reff, level, fragile));
        Save(arrivals);
        using (var key = Registry.CurrentUser.CreateSubKey(SettingsKey)) key.SetValue("serviceLevel", level);
        Status.Text = $"Registered {reff}: {level}{(fragile ? ", fragile" : "")}{(PrintLabel.IsOn ? ", label printed" : "")}";
        Reference.Text = "";
        Fragile.IsChecked = false;
        Refresh();
    }

    void OnArrival(object sender, ItemClickEventArgs e)
    {
        var a = arrivals.First(x => x.Reference == (string)e.ClickedItem);
        Status.Text = $"{a.Reference}: {a.Level}{(a.Fragile ? ", fragile" : "")}";
    }

    void OnExpected(object sender, ItemClickEventArgs e) => Reference.Text = (string)((Grid)e.ClickedItem).Tag;

    void OnCloseDay(object sender, RoutedEventArgs e)
    {
        var n = arrivals.Count;
        arrivals = [];
        Save(arrivals);
        Status.Text = $"Day closed: {Parcels(n)} handed over";
        Refresh();
    }

    void OnClearSignature(object sender, RoutedEventArgs e)
    {
        Pad.Clear();
        Signed.Text = "Not signed";
    }

    void OnRules(object sender, RoutedEventArgs e) => RulesText.Visibility = Visibility.Visible;
}
