using Microsoft.UI.Xaml;

namespace DepotDesk;

public partial class App : Application
{
    Window? desk;

    public App() => InitializeComponent();

    protected override void OnLaunched(LaunchActivatedEventArgs args)
    {
        desk = new Desk();
        desk.Activate();
    }
}
