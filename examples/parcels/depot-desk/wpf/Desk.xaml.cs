// The depot desk in WPF (../README.md).
using System.IO;
using System.Text.Json;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using System.Windows.Media.Imaging;

namespace DepotDesk;

public record Arrival(string Reference, string Level, bool Fragile);

public record ExpectedParcel(string Reference, string Town);

public partial class Desk : Window
{
    static readonly string[] Towns = ["Leipzig", "Halle", "Markkleeberg", "Taucha", "Schkeuditz", "Delitzsch", "Borna", "Grimma", "Wurzen", "Eilenburg"];
    static string DataFolder => Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData), "Parcels", "Depot desk");
    static string SettingsFolder => Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "Parcels", "Depot desk");

    List<Arrival> arrivals;

    static string Parcels(int n) => n == 1 ? "1 parcel" : $"{n} parcels";

    public Desk()
    {
        InitializeComponent();
        Logo.Source = new BitmapImage(new Uri(Path.Combine(AppContext.BaseDirectory, "depot.png")));
        arrivals = Load();
        (LevelKept() == "Express" ? Express : Standard).IsChecked = true;
        Expected.ItemsSource = Enumerable.Range(0, 40).Select(i => new ExpectedParcel($"PX-DSK-{4101 + i}", Towns[i % Towns.Length])).ToList();
        Refresh();
        Status.Text = arrivals.Count == 0 ? "No parcels registered yet" : $"{Parcels(arrivals.Count)} registered today";
    }

    static List<Arrival> Load()
    {
        try { return JsonSerializer.Deserialize<List<Arrival>>(File.ReadAllText(Path.Combine(DataFolder, "arrivals.json"))) ?? []; }
        catch (IOException) { return []; }
    }

    static void Save(List<Arrival> arrivals)
    {
        Directory.CreateDirectory(DataFolder);
        File.WriteAllText(Path.Combine(DataFolder, "arrivals.json"), JsonSerializer.Serialize(arrivals));
    }

    static string LevelKept()
    {
        try { return JsonSerializer.Deserialize<Dictionary<string, string>>(File.ReadAllText(Path.Combine(SettingsFolder, "settings.json")))?["serviceLevel"] ?? "Standard"; }
        catch (Exception e) when (e is IOException or KeyNotFoundException) { return "Standard"; }
    }

    void Refresh()
    {
        RegisterButton.IsEnabled = Reference.Text.Trim().Length > 0;
        CloseDayItem.IsEnabled = arrivals.Count > 0;
        ArrivalsList.ItemsSource = arrivals.Select(a => a.Reference).ToList();
    }

    void OnReferenceChanged(object sender, TextChangedEventArgs e) => Refresh();

    void OnReferenceKey(object sender, KeyEventArgs e)
    {
        if (e.Key == Key.Enter) { Register(); e.Handled = true; }
        if (e.Key == Key.Escape) { Reference.Clear(); e.Handled = true; }
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
        Directory.CreateDirectory(SettingsFolder);
        File.WriteAllText(Path.Combine(SettingsFolder, "settings.json"), JsonSerializer.Serialize(new Dictionary<string, string> { ["serviceLevel"] = level }));
        Status.Text = $"Registered {reff}: {level}{(fragile ? ", fragile" : "")}{(PrintLabel.IsChecked == true ? ", label printed" : "")}";
        Reference.Clear();
        Fragile.IsChecked = false;
        Refresh();
    }

    void OnArrival(object sender, SelectionChangedEventArgs e)
    {
        if (ArrivalsList.SelectedIndex < 0) return;
        var a = arrivals[ArrivalsList.SelectedIndex];
        Status.Text = $"{a.Reference}: {a.Level}{(a.Fragile ? ", fragile" : "")}";
        Dispatcher.BeginInvoke(() => ArrivalsList.SelectedIndex = -1);
    }

    void OnExpected(object sender, SelectionChangedEventArgs e)
    {
        if (Expected.SelectedItem is not ExpectedParcel p) return;
        Reference.Text = p.Reference;
        Dispatcher.BeginInvoke(() => Expected.SelectedIndex = -1);
    }

    void OnCloseDay(object sender, RoutedEventArgs e)
    {
        var n = arrivals.Count;
        arrivals = [];
        Save(arrivals);
        Status.Text = $"Day closed: {Parcels(n)} handed over";
        Refresh();
    }

    void OnSigned(object sender, InkCanvasStrokeCollectedEventArgs e) => Signed.Text = "Signed";

    void OnClearSignature(object sender, RoutedEventArgs e)
    {
        Pad.Strokes.Clear();
        Signed.Text = "Not signed";
    }

    void OnRules(object sender, RoutedEventArgs e) => RulesText.Visibility = Visibility.Visible;
}
