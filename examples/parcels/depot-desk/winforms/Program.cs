// The depot desk in Windows Forms (../README.md).
using System.Text.Json;
using Microsoft.Win32;

namespace DepotDesk;

record Arrival(string Reference, string Level, bool Fragile);

static class Store
{
    static string Folder => Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData), "Parcels", "Depot desk");
    static string File => Path.Combine(Folder, "arrivals.json");

    public static List<Arrival> Load()
    {
        try { return JsonSerializer.Deserialize<List<Arrival>>(System.IO.File.ReadAllText(File)) ?? []; }
        catch (IOException) { return []; }
    }

    public static void Save(List<Arrival> arrivals)
    {
        Directory.CreateDirectory(Folder);
        System.IO.File.WriteAllText(File, JsonSerializer.Serialize(arrivals));
    }
}

/// <summary>Where the courier signs: a click puts a dot, a drag draws a stroke.</summary>
class SignaturePad : Control
{
    readonly List<List<Point>> strokes = [];
    public event Action? Signed;

    public SignaturePad()
    {
        Size = new Size(400, 160);
        BackColor = Color.White;
        DoubleBuffered = true;
        AccessibleName = "Courier signature";
        AccessibleRole = AccessibleRole.Graphic;
    }

    protected override void OnMouseDown(MouseEventArgs e)
    {
        strokes.Add([e.Location]);
        Invalidate();
        Signed?.Invoke();
    }

    protected override void OnMouseMove(MouseEventArgs e)
    {
        if (e.Button != MouseButtons.Left || strokes.Count == 0) return;
        strokes[^1].Add(e.Location);
        Invalidate();
    }

    protected override void OnPaint(PaintEventArgs e)
    {
        using var pen = new Pen(Color.Black, 3) { StartCap = System.Drawing.Drawing2D.LineCap.Round, EndCap = System.Drawing.Drawing2D.LineCap.Round };
        e.Graphics.SmoothingMode = System.Drawing.Drawing2D.SmoothingMode.AntiAlias;
        foreach (var s in strokes)
        {
            if (s.Count == 1) e.Graphics.FillEllipse(Brushes.Black, s[0].X - 2, s[0].Y - 2, 4, 4);
            for (var i = 1; i < s.Count; i++) e.Graphics.DrawLine(pen, s[i - 1], s[i]);
        }
    }

    public void Clear()
    {
        strokes.Clear();
        Invalidate();
    }
}

class Desk : Form
{
    static readonly string[] Towns = ["Leipzig", "Halle", "Markkleeberg", "Taucha", "Schkeuditz", "Delitzsch", "Borna", "Grimma", "Wurzen", "Eilenburg"];
    const string SettingsKey = @"Software\Parcels\Depot desk";

    List<Arrival> arrivals = Store.Load();
    readonly ToolStripMenuItem closeDay = new("Close day");
    readonly TextBox reference = new() { AccessibleName = "Reference", Width = 340 };
    readonly CheckBox fragile = new() { Text = "Fragile", AutoSize = true };
    readonly RadioButton standard = new() { Text = "Standard", AutoSize = true };
    readonly RadioButton express = new() { Text = "Express", AutoSize = true };
    readonly CheckBox printLabel = new() { Text = "Print label", AutoSize = true }; // Windows Forms has no switch
    readonly Button register = new() { Text = "Register", AutoSize = true };
    readonly Label status = new() { AutoSize = true };
    readonly ListBox list = new() { AccessibleName = "Arrivals", Width = 360, Height = 160 };
    readonly SignaturePad pad = new();
    readonly Label signed = new() { Text = "Not signed", AutoSize = true };
    readonly Label rulesText = new() { Text = "Parcels are handed over to the courier at 18:00.", AutoSize = true, Visible = false };

    static string Parcels(int n) => n == 1 ? "1 parcel" : $"{n} parcels";

    public Desk()
    {
        Text = "Depot desk";
        AutoScaleMode = AutoScaleMode.Dpi;
        AutoScaleDimensions = new SizeF(96, 96);
        Size = new Size(640, 580); // the window, its title bar in it

        closeDay.Click += (_, _) => CloseDay();
        var depot = new ToolStripMenuItem("Depot");
        depot.DropDownItems.Add(closeDay);
        var menu = new MenuStrip();
        menu.Items.Add(depot);
        MainMenuStrip = menu;

        var logo = new PictureBox { Image = Image.FromFile(Path.Combine(AppContext.BaseDirectory, "depot.png")), SizeMode = PictureBoxSizeMode.AutoSize, AccessibleName = "Leipzig depot", AccessibleRole = AccessibleRole.Graphic };
        var tabs = new TabControl { Dock = DockStyle.Fill };
        tabs.TabPages.Add(ArrivalsTab());
        tabs.TabPages.Add(HandoverTab());

        // The tabs fill the window under the menu and the logo (the last docked first).
        var top = new FlowLayoutPanel { Dock = DockStyle.Top, AutoSize = true, Padding = new Padding(12, 12, 12, 0), Controls = { logo } };
        Controls.Add(tabs);
        Controls.Add(top);
        Controls.Add(menu);
        Padding = new Padding(12, 0, 12, 12);
        Refresh2();
        status.Text = arrivals.Count == 0 ? "No parcels registered yet" : $"{Parcels(arrivals.Count)} registered today";
    }

    TabPage ArrivalsTab()
    {
        var page = new TabPage("Arrivals");
        var label = new Label { Text = "Reference", AutoSize = true };
        reference.TextChanged += (_, _) => Refresh2();
        reference.KeyDown += (_, e) =>
        {
            if (e.KeyCode == Keys.Enter) { Register(); e.SuppressKeyPress = true; }
            if (e.KeyCode == Keys.Escape) { reference.Clear(); e.SuppressKeyPress = true; }
        };
        (LevelKept() == "Express" ? express : standard).Checked = true;
        var levels = new FlowLayoutPanel { AutoSize = true, Controls = { standard, express } };
        register.Click += (_, _) => Register();
        list.Click += (_, _) =>
        {
            if (list.SelectedIndex < 0) return;
            var a = arrivals[list.SelectedIndex];
            status.Text = $"{a.Reference}: {a.Level}{(a.Fragile ? ", fragile" : "")}";
            list.ClearSelected();
        };
        var expected = new ListView { View = View.Details, FullRowSelect = true, MultiSelect = false, Width = 360, Height = 8 * 18 + 28, AccessibleName = "Expected today", HideSelection = true };
        expected.Columns.Add("Reference", 170);
        expected.Columns.Add("Town", 170);
        for (var i = 0; i < 40; i++) expected.Items.Add(new ListViewItem([$"PX-DSK-{4101 + i}", Towns[i % Towns.Length]]));
        expected.ItemSelectionChanged += (_, e) =>
        {
            if (!e.IsSelected) return;
            reference.Text = e.Item!.Text;
            BeginInvoke(() => e.Item.Selected = false);
        };
        // One column, taller than the window: it scrolls, as a page does.
        var column = new FlowLayoutPanel { Dock = DockStyle.Fill, FlowDirection = FlowDirection.TopDown, WrapContents = false, AutoScroll = true, Padding = new Padding(8) };
        column.Controls.AddRange([label, reference, fragile, levels, printLabel, register, status,
            new Label { Text = "Arrivals", AutoSize = true }, list, new Label { Text = "Expected today", AutoSize = true }, expected]);
        page.Controls.Add(column);
        return page;
    }

    TabPage HandoverTab()
    {
        var page = new TabPage("Handover");
        pad.Signed += () => signed.Text = "Signed";
        var clear = new Button { Text = "Clear signature", AutoSize = true };
        clear.Click += (_, _) => { pad.Clear(); signed.Text = "Not signed"; };
        var rules = new LinkLabel { Text = "Handover rules", AutoSize = true };
        rules.LinkClicked += (_, _) => rulesText.Visible = true;
        var flow = new FlowLayoutPanel { Dock = DockStyle.Fill, FlowDirection = FlowDirection.TopDown, WrapContents = false, Padding = new Padding(8) };
        flow.Controls.AddRange([pad, signed, clear, rules, rulesText]);
        page.Controls.Add(flow);
        return page;
    }

    static string LevelKept() => Registry.CurrentUser.OpenSubKey(SettingsKey)?.GetValue("serviceLevel") as string ?? "Standard";

    void Refresh2()
    {
        register.Enabled = reference.Text.Trim().Length > 0;
        closeDay.Enabled = arrivals.Count > 0;
        list.Items.Clear();
        foreach (var a in arrivals) list.Items.Add(a.Reference);
    }

    void Register()
    {
        var reff = reference.Text.Trim();
        if (reff.Length == 0) return;
        if (arrivals.Any(a => a.Reference == reff))
        {
            status.Text = $"{reff} is already registered";
            return;
        }
        var level = express.Checked ? "Express" : "Standard";
        arrivals.Add(new Arrival(reff, level, fragile.Checked));
        Store.Save(arrivals);
        using (var key = Registry.CurrentUser.CreateSubKey(SettingsKey)) key.SetValue("serviceLevel", level);
        status.Text = $"Registered {reff}: {level}{(fragile.Checked ? ", fragile" : "")}{(printLabel.Checked ? ", label printed" : "")}";
        reference.Clear();
        fragile.Checked = false;
        Refresh2();
    }

    void CloseDay()
    {
        var n = arrivals.Count;
        arrivals = [];
        Store.Save(arrivals);
        status.Text = $"Day closed: {Parcels(n)} handed over";
        Refresh2();
    }

    [STAThread]
    static void Main()
    {
        ApplicationConfiguration.Initialize();
        Application.Run(new Desk());
    }
}
