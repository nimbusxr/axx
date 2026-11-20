// The app of the toast test: a courier confirms a delivery, and the app says so in a toast.
package example.parcels.receipts;

import android.app.Activity;
import android.os.Bundle;
import android.widget.Button;
import android.widget.LinearLayout;
import android.widget.TextView;
import android.widget.Toast;

public class Receipts extends Activity {
    @Override
    protected void onCreate(Bundle state) {
        super.onCreate(state);
        LinearLayout column = new LinearLayout(this);
        column.setOrientation(LinearLayout.VERTICAL);
        // Below the status bar, which an app of this target SDK draws under.
        column.setFitsSystemWindows(true);
        TextView parcel = new TextView(this);
        parcel.setText("Parcel 4471 is at the door");
        Button confirm = new Button(this);
        confirm.setText("Confirm delivery");
        confirm.setOnClickListener(v -> Toast.makeText(this, "Delivery confirmed", Toast.LENGTH_LONG).show());
        Button next = new Button(this);
        next.setText("Next parcel");
        next.setOnClickListener(v -> parcel.setText("Parcel 4472 is next"));
        column.addView(parcel);
        column.addView(confirm);
        column.addView(next);
        setContentView(column);
    }
}
