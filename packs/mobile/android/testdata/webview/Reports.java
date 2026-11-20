// The app of the web view tests: one web view, with the page among its assets.
package example.parcels.reports;

import android.app.Activity;
import android.os.Bundle;
import android.webkit.WebView;
import android.widget.FrameLayout;

public class Reports extends Activity {
    @Override
    protected void onCreate(Bundle state) {
        super.onCreate(state);
        WebView view = new WebView(this);
        view.getSettings().setJavaScriptEnabled(true);
        // The page is shown between the system's bars, as an app of this target SDK must see to
        // itself: it draws under them otherwise, and Android says of a control under the status
        // bar that it does not show (a page's heading, on a device whose bar is tall enough to
        // cover it: a test then waits in vain for a heading that is on the screen).
        FrameLayout between = new FrameLayout(this);
        between.setFitsSystemWindows(true);
        between.addView(view);
        setContentView(between);
        view.loadUrl("file:///android_asset/page.html");
    }
}
