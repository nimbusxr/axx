// The app of the web view tests: one web view, with the page among its assets.
package example.parcels.reports;

import android.app.Activity;
import android.os.Bundle;
import android.webkit.WebView;

public class Reports extends Activity {
    @Override
    protected void onCreate(Bundle state) {
        super.onCreate(state);
        WebView view = new WebView(this);
        view.getSettings().setJavaScriptEnabled(true);
        setContentView(view);
        view.loadUrl("file:///android_asset/report.html");
    }
}
