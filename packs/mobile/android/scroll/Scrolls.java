// SPDX-License-Identifier: Apache-2.0

package us.nimbusxr.axx.scroll;

import android.accessibilityservice.AccessibilityService;
import android.graphics.Rect;
import android.net.LocalServerSocket;
import android.net.LocalSocket;
import android.os.SystemClock;
import android.util.Log;
import android.view.accessibility.AccessibilityEvent;
import android.view.accessibility.AccessibilityNodeInfo;
import android.view.accessibility.AccessibilityNodeInfo.AccessibilityAction;
import android.view.accessibility.AccessibilityWindowInfo;
import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;
import org.json.JSONArray;
import org.json.JSONObject;

/**
 * axx's scroll service. A scroll is what a view does when it is asked to: Android's accessibility
 * actions, which a screen reader uses, ask a view to scroll by a page or to bring one of its
 * controls onto the screen, with no finger on the glass. The UiAutomator2 server axx drives a
 * device with has no command for them, so this service performs them beside it.
 *
 * <p>axx talks to it over adb, a line of JSON a request and a line an answer, on the socket
 * {@link #SOCKET}. A request names a control as axx read it from the screen (its class, text,
 * description, resource ID and bounds, and which of several alike at one place it is) and what to do: "show" it, "scroll" it a page toward a
 * direction, or say "where" it is. The answer says whether the control was found, whether it took the action, and
 * whether anything moved.
 */
public class Scrolls extends AccessibilityService {
    /** The socket's name, in Android's abstract namespace: adb forwards a port of the machine to it. */
    public static final String SOCKET = "axx-scroll";

    /** The protocol's version, which a ping answers: axx installs the service again when it differs. */
    static final int VERSION = 1;

    private static final String TAG = "axx-scroll";

    // Android makes the service anew each time it connects it to the screen (as it is switched on,
    // and again after a test that took accessibility for itself has gone), in one process. The
    // socket and what came of scrolls are the process's, so that axx's connection outlives an
    // instance: a request is answered by the instance connected now.
    private static final Object MOVES = new Object();
    private static long moved; // how many scroll events came, guarded by MOVES
    private static long movedAt; // when the last one came, guarded by MOVES
    private static long counted; // how many had come when the last action was asked, guarded by MOVES
    private static long firstAt; // when the first one after it came, guarded by MOVES
    private static volatile Scrolls connected; // the instance Android has reading the screen now
    private static boolean serving; // whether the socket is served, guarded by MOVES

    @Override
    protected void onServiceConnected() {
        Log.i(TAG, "connected to the screen");
        connected = this;
        synchronized (MOVES) {
            if (serving) {
                return;
            }
            serving = true;
        }
        Thread t = new Thread(Scrolls::serve, "axx-scroll");
        t.setDaemon(true);
        t.start();
    }

    @Override
    public boolean onUnbind(android.content.Intent intent) {
        Log.i(TAG, "disconnected from the screen");
        if (connected == this) {
            connected = null;
        }
        return super.onUnbind(intent);
    }

    @Override
    public void onAccessibilityEvent(AccessibilityEvent event) {
        if (event.getEventType() == AccessibilityEvent.TYPE_VIEW_SCROLLED) {
            synchronized (MOVES) {
                movedAt = SystemClock.uptimeMillis();
                if (moved++ == counted) {
                    firstAt = movedAt;
                }
                MOVES.notifyAll();
            }
        }
    }

    @Override
    public void onInterrupt() {}

    private static void serve() {
        try {
            LocalServerSocket server = new LocalServerSocket(SOCKET);
            while (true) {
                final LocalSocket client = server.accept();
                Thread t = new Thread(() -> talk(client), "axx-scroll-client");
                t.setDaemon(true);
                t.start();
            }
        } catch (Exception e) {
            // The socket is another process's, or gone: a service connected later serves it anew.
            Log.w(TAG, "the socket is not served", e);
            synchronized (MOVES) {
                serving = false;
            }
        }
    }

    private static void talk(LocalSocket client) {
        try (LocalSocket c = client) {
            BufferedReader in = new BufferedReader(new InputStreamReader(c.getInputStream(), StandardCharsets.UTF_8));
            OutputStream out = c.getOutputStream();
            for (String line = in.readLine(); line != null; line = in.readLine()) {
                JSONObject answer;
                try {
                    answer = handle(new JSONObject(line));
                    // The device's log keeps what was asked and answered, for whoever reads why a
                    // step scrolled as it did.
                    Log.i(TAG, line + " -> " + answer);
                } catch (Throwable e) {
                    Log.w(TAG, line + " failed", e);
                    // A service Android has paused (a test that takes accessibility for itself)
                    // cannot read the screen: axx is told so, and scrolls another way.
                    answer = new JSONObject().put("error", String.valueOf(e));
                }
                out.write((answer.toString() + "\n").getBytes(StandardCharsets.UTF_8));
                out.flush();
            }
        } catch (Exception e) {
            // axx hung up.
        }
    }

    private static JSONObject handle(JSONObject req) throws Exception {
        JSONObject answer = new JSONObject();
        String op = req.optString("op");
        Scrolls service = connected;
        if (op.equals("ping")) {
            return answer.put("version", VERSION).put("connected", service != null);
        }
        if (service == null) {
            return answer.put("error", "Android has not connected the scroll service to the screen");
        }
        AccessibilityNodeInfo node = service.find(req);
        answer.put("found", node != null);
        if (node == null) {
            return answer;
        }
        JSONArray offered = new JSONArray();
        for (AccessibilityAction a : node.getActionList()) {
            offered.put(a.getId());
        }
        answer.put("actions", offered);
        int action = 0;
        switch (op) {
            case "show":
                action = AccessibilityAction.ACTION_SHOW_ON_SCREEN.getId();
                break;
            case "scroll":
                action = scrollAction(node, req.optString("direction"));
                break;
            case "where":
                return answer.put("bounds", bounds(node));
            default:
                return answer.put("error", "unknown op " + op);
        }
        answer.put("offered", action != 0 && offers(node, action));
        answer.put("scrolls", scrolls(node));
        if (action == 0) {
            return answer.put("performed", false).put("moved", false);
        }
        long before;
        synchronized (MOVES) {
            before = moved;
            counted = moved;
        }
        long asked = SystemClock.uptimeMillis();
        boolean performed = node.performAction(action);
        answer.put("performed", performed);
        answer.put("moved", performed && settled(before));
        // How long the view took to say it scrolled, for whoever reads the device's log.
        synchronized (MOVES) {
            if (moved != before) {
                answer.put("after", firstAt - asked);
            }
        }
        node.refresh();
        return answer.put("bounds", bounds(node));
    }

    /**
     * The action that scrolls a view a page toward a direction: the one of that direction when
     * the view offers it, or else forward (down, right) or backward (up, left), which every
     * view that scrolls takes along the way it scrolls.
     */
    private static int scrollAction(AccessibilityNodeInfo node, String direction) {
        int exact;
        int along;
        switch (direction) {
            case "down":
                exact = AccessibilityAction.ACTION_SCROLL_DOWN.getId();
                along = AccessibilityNodeInfo.ACTION_SCROLL_FORWARD;
                break;
            case "up":
                exact = AccessibilityAction.ACTION_SCROLL_UP.getId();
                along = AccessibilityNodeInfo.ACTION_SCROLL_BACKWARD;
                break;
            case "right":
                exact = AccessibilityAction.ACTION_SCROLL_RIGHT.getId();
                along = AccessibilityNodeInfo.ACTION_SCROLL_FORWARD;
                break;
            case "left":
                exact = AccessibilityAction.ACTION_SCROLL_LEFT.getId();
                along = AccessibilityNodeInfo.ACTION_SCROLL_BACKWARD;
                break;
            default:
                return 0;
        }
        if (offers(node, exact)) {
            return exact;
        }
        // A view that names directions and does not offer this one is at its end that way, or
        // scrolls the other way: forward on a page that scrolls down is not "right".
        for (AccessibilityAction a : DIRECTIONS) {
            if (offers(node, a.getId())) {
                return 0;
            }
        }
        return offers(node, along) ? along : 0;
    }

    private static final AccessibilityAction[] DIRECTIONS = {
        AccessibilityAction.ACTION_SCROLL_DOWN, AccessibilityAction.ACTION_SCROLL_UP,
        AccessibilityAction.ACTION_SCROLL_LEFT, AccessibilityAction.ACTION_SCROLL_RIGHT,
    };

    /** Whether the view offers any scroll at all: one that offers none is scrolled by a finger only. */
    private static boolean scrolls(AccessibilityNodeInfo node) {
        if (offers(node, AccessibilityNodeInfo.ACTION_SCROLL_FORWARD) || offers(node, AccessibilityNodeInfo.ACTION_SCROLL_BACKWARD)) {
            return true;
        }
        for (AccessibilityAction a : DIRECTIONS) {
            if (offers(node, a.getId())) {
                return true;
            }
        }
        return false;
    }

    private static boolean offers(AccessibilityNodeInfo node, int action) {
        for (AccessibilityAction a : node.getActionList()) {
            if (a.getId() == action) {
                return true;
            }
        }
        return false;
    }

    /**
     * Waits for a scroll the action started to end, and reports whether there was one: a view
     * says it scrolled a moment after it was asked (300 ms at most), and is at rest once it has
     * said nothing more for a moment (two seconds at most, for one that animates).
     */
    private static boolean settled(long before) throws InterruptedException {
        long start = SystemClock.uptimeMillis();
        synchronized (MOVES) {
            while (moved == before) {
                long left = 300 - (SystemClock.uptimeMillis() - start);
                if (left <= 0) {
                    return false;
                }
                MOVES.wait(left);
            }
            while (SystemClock.uptimeMillis() - start < 2000) {
                long quiet = 80 - (SystemClock.uptimeMillis() - movedAt);
                if (quiet <= 0) {
                    break;
                }
                MOVES.wait(quiet);
            }
        }
        return true;
    }

    private static JSONArray bounds(AccessibilityNodeInfo node) {
        Rect r = new Rect();
        node.getBoundsInScreen(r);
        return new JSONArray().put(r.left).put(r.top).put(r.right).put(r.bottom);
    }

    /**
     * The control a request names: of its class, text, description and resource ID, the one
     * whose place is nearest the bounds axx read, and of several there, the one axx says it is.
     * (The server axx reads the screen with cuts a control's bounds to the views it scrolls in,
     * so they are near, not always the same.)
     */
    private AccessibilityNodeInfo find(JSONObject req) {
        List<AccessibilityNodeInfo> roots = new ArrayList<>();
        for (AccessibilityWindowInfo w : getWindows()) {
            AccessibilityNodeInfo root = w.getRoot();
            if (root != null) {
                roots.add(root);
            }
        }
        if (roots.isEmpty()) {
            AccessibilityNodeInfo root = getRootInActiveWindow();
            if (root != null) {
                roots.add(root);
            }
        }
        JSONArray b = req.optJSONArray("bounds");
        String pkg = req.optString("package");
        AccessibilityNodeInfo best = null;
        long bestOff = Long.MAX_VALUE;
        Rect r = new Rect();
        List<AccessibilityNodeInfo> all = new ArrayList<>();
        for (AccessibilityNodeInfo root : roots) {
            if (!pkg.isEmpty() && !pkg.contentEquals(orEmpty(root.getPackageName()))) {
                continue;
            }
            walk(root, all, 0);
        }
        int nth = req.optInt("nth", -1);
        int of = req.optInt("of");
        // By its text too, first; then without it, at the very place axx read: the server gives
        // a control with a range (a slider, a web page's number field) its number for a text.
        for (boolean byText : new boolean[] {true, false}) {
            // Those alike that are nearest, in the screen's order: several, when they are at one place.
            List<AccessibilityNodeInfo> nearest = new ArrayList<>();
            for (AccessibilityNodeInfo n : all) {
                if (!same(req, "class", n.getClassName()) || (byText && !same(req, "text", n.getText()))
                        || !same(req, "desc", n.getContentDescription()) || !same(req, "id", n.getViewIdResourceName())) {
                    continue;
                }
                long off = 0;
                if (b != null && b.length() == 4) {
                    n.getBoundsInScreen(r);
                    off = Math.abs((long) r.left - b.optInt(0)) + Math.abs((long) r.top - b.optInt(1))
                            + Math.abs((long) r.right - b.optInt(2)) + Math.abs((long) r.bottom - b.optInt(3));
                }
                if (!byText && off > 8) {
                    continue;
                }
                if (off < bestOff) {
                    best = n;
                    bestOff = off;
                    nearest.clear();
                }
                if (off == bestOff) {
                    nearest.add(n);
                }
            }
            // Several alike at one place, and axx said which of them it means, in the screen's
            // order: that one, while the screen still has as many there. Out of view, a web page
            // gives every control one place, cut to the screen's edge, so where they are does
            // not tell them apart.
            if (byText && nth >= 0 && nth < of && nearest.size() == of) {
                return nearest.get(nth);
            }
            if (best != null || !req.has("text")) {
                break;
            }
        }
        return best;
    }

    private static CharSequence orEmpty(CharSequence s) {
        return s == null ? "" : s;
    }

    /** Whether the control has what the request names of it; a request that names nothing of a kind takes any. */
    private static boolean same(JSONObject req, String key, CharSequence has) {
        if (!req.has(key)) {
            return true;
        }
        return req.optString(key).contentEquals(orEmpty(has));
    }

    private static void walk(AccessibilityNodeInfo n, List<AccessibilityNodeInfo> out, int depth) {
        if (n == null || depth > 80) {
            return;
        }
        out.add(n);
        for (int i = 0; i < n.getChildCount(); i++) {
            walk(n.getChild(i), out, depth + 1);
        }
    }
}
