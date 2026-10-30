// SPDX-License-Identifier: Apache-2.0

package us.nimbusxr.axx.ar;

import android.content.Context;
import android.graphics.SurfaceTexture;
import android.hardware.camera2.CameraCaptureSession;
import android.hardware.camera2.CameraCharacteristics;
import android.hardware.camera2.CameraDevice;
import android.hardware.camera2.CameraManager;
import android.hardware.camera2.CaptureRequest;
import android.net.LocalServerSocket;
import android.net.LocalSocket;
import android.os.Handler;
import android.os.HandlerThread;
import android.util.Base64;
import android.util.Log;
import android.view.MotionEvent;
import android.view.Surface;
import android.view.SurfaceView;
import android.view.View;
import android.view.ViewGroup;
import java.io.BufferedReader;
import java.io.File;
import java.io.FileInputStream;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.nio.ByteBuffer;
import java.nio.ByteOrder;
import java.nio.FloatBuffer;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.HashMap;
import java.util.IdentityHashMap;
import java.util.List;
import java.util.Map;
import org.json.JSONArray;
import org.json.JSONObject;

/**
 * Answers an app's ARCore calls from the scene its camera sees (agent.cc hands them over): a
 * session that runs on any emulator, its camera at the phone's pose in the scene, which axx sets,
 * its picture the emulator's camera's, which renders the scene from there, and its hit tests
 * landing on the scene's surfaces, and on a named object where a tap on it lands. The camera is
 * the emulator's: its sensor 1280 by 960, turned a quarter (a phone's back camera), a pinhole
 * whose focal length is 960 pixels, the picture filling the view as ARCore fills it.
 */
public final class Bridge {
  /** What the bridge answers for a call that is to run as it would. */
  public static final Object PROCEED = new Object();

  static native Object allocate(Class<?> type);

  private static final String TAG = "AXXAR";
  // The camera: its sensor's picture, its focal length, and the picture held upright.
  private static final float SENSOR_W = 1280, SENSOR_H = 960, FOCAL = 960;
  private static final float UPRIGHT_W = SENSOR_H, UPRIGHT_H = SENSOR_W;

  private static String dir;
  private static ClassLoader loader;
  private static final Map<String, Class<?>> classes = new HashMap<>();

  // The scene: its triangles in the world, each with the object it is part of (-1 for none),
  // and the objects' names and where a tap on each lands.
  private static float[] triangles = new float[0];
  private static int[] triangleObject = new int[0];
  private static final List<String> objectNames = new ArrayList<>();
  private static final List<float[]> objectTaps = new ArrayList<>();
  /** The camera's world transform, column by column: the phone held upright, looking along -Z. */
  private static volatile float[] pose = identity();

  // The session's state.
  private static Context context;
  private static int texture = -1;
  private static int rotation;
  private static int viewW = 1, viewH = 1;
  private static boolean geometryChanged;
  private static SurfaceTexture picture;
  private static Surface pictureSurface;
  private static CameraDevice cameraDevice;
  private static Handler cameraHandler;
  private static final float[] textureMatrix = identity();
  private static long timestamp;
  private static Object config, frame, camera, lightEstimate;
  private static boolean frameGeometryChanged;
  private static final Map<Object, float[]> anchors = new IdentityHashMap<>();
  private static final Map<Object, Hit> hits = new IdentityHashMap<>();
  private static final Map<Object, float[]> planes = new IdentityHashMap<>();
  private static final Map<Object, Map<String, Object>> configs = new IdentityHashMap<>();

  /** Where a hit test hit: the point, its surface's normal, and how far from the camera. */
  private static final class Hit {
    final float[] point, normal;
    final float distance;
    final Object plane;

    Hit(float[] point, float[] normal, float distance, Object plane) {
      this.point = point;
      this.normal = normal;
      this.distance = distance;
      this.plane = plane;
    }
  }

  public static void initialize(String folder) throws Exception {
    dir = folder;
    JSONObject scene = new JSONObject(read(new File(dir, "scene.json")));
    loadScene(scene);
    File posed = new File(dir, "pose");
    pose = posed.exists() ? floats(read(posed)) : matrix(scene.getJSONArray("phone"));
    String name = read(new File(dir, "socket")).trim();
    LocalServerSocket server = new LocalServerSocket(name);
    Thread t = new Thread(() -> serve(server), "axx-ar-control");
    t.setDaemon(true);
    t.start();
    Log.i(TAG, "ready: " + objectNames.size() + " objects, " + triangles.length / 9 + " triangles, control " + name);
  }

  private static String read(File f) throws Exception {
    try (FileInputStream in = new FileInputStream(f)) {
      byte[] b = new byte[(int) f.length()];
      int n = 0;
      while (n < b.length) {
        int r = in.read(b, n, b.length - n);
        if (r < 0) break;
        n += r;
      }
      return new String(b, 0, n, StandardCharsets.UTF_8);
    }
  }

  // ---- the scene

  private static void loadScene(JSONObject scene) throws Exception {
    JSONArray nodes = scene.getJSONArray("nodes");
    int n = nodes.length();
    int[] parent = new int[n];
    Arrays.fill(parent, -1);
    for (int i = 0; i < n; i++) {
      JSONArray children = nodes.getJSONObject(i).optJSONArray("children");
      for (int c = 0; children != null && c < children.length(); c++) parent[children.getInt(c)] = i;
    }
    int[] object = new int[n];
    Arrays.fill(object, -1);
    JSONArray objects = scene.getJSONArray("objects");
    for (int o = 0; o < objects.length(); o++) {
      JSONObject obj = objects.getJSONObject(o);
      objectNames.add(obj.getString("name"));
      JSONArray tap = obj.getJSONArray("tap");
      objectTaps.add(new float[] {(float) tap.getDouble(0), (float) tap.getDouble(1), (float) tap.getDouble(2)});
      object[obj.getInt("node")] = o;
    }
    float[][] world = new float[n][];
    List<Float> tris = new ArrayList<>();
    List<Integer> owners = new ArrayList<>();
    for (int i = 0; i < n; i++) {
      float[] m = worldOf(i, nodes, parent, world);
      int owner = -1;
      for (int p = i; p >= 0 && owner < 0; p = parent[p]) owner = object[p];
      JSONArray meshes = nodes.getJSONObject(i).optJSONArray("meshes");
      for (int k = 0; meshes != null && k < meshes.length(); k++) {
        JSONObject mesh = meshes.getJSONObject(k);
        float[] pos = decodeFloats(mesh.getString("positions"));
        int[] idx = decodeInts(mesh.getString("indices"));
        for (int t = 0; t + 2 < idx.length; t += 3) {
          for (int v = 0; v < 3; v++) {
            float[] p = transform(m, pos[3 * idx[t + v]], pos[3 * idx[t + v] + 1], pos[3 * idx[t + v] + 2]);
            tris.add(p[0]);
            tris.add(p[1]);
            tris.add(p[2]);
          }
          owners.add(owner);
        }
      }
    }
    triangles = new float[tris.size()];
    for (int i = 0; i < triangles.length; i++) triangles[i] = tris.get(i);
    triangleObject = new int[owners.size()];
    for (int i = 0; i < triangleObject.length; i++) triangleObject[i] = owners.get(i);
  }

  private static float[] worldOf(int i, JSONArray nodes, int[] parent, float[][] world) throws Exception {
    if (world[i] != null) return world[i];
    float[] local = matrix(nodes.getJSONObject(i).getJSONArray("matrix"));
    world[i] = parent[i] < 0 ? local : multiply(worldOf(parent[i], nodes, parent, world), local);
    return world[i];
  }

  private static float[] decodeFloats(String b64) {
    if (b64.isEmpty()) return new float[0];
    FloatBuffer fb = ByteBuffer.wrap(Base64.decode(b64, Base64.DEFAULT)).order(ByteOrder.LITTLE_ENDIAN).asFloatBuffer();
    float[] out = new float[fb.remaining()];
    fb.get(out);
    return out;
  }

  private static int[] decodeInts(String b64) {
    ByteBuffer bb = ByteBuffer.wrap(Base64.decode(b64, Base64.DEFAULT)).order(ByteOrder.LITTLE_ENDIAN);
    int[] out = new int[bb.remaining() / 4];
    for (int i = 0; i < out.length; i++) out[i] = bb.getInt();
    return out;
  }

  // ---- the control socket axx reaches through adb forward: where the phone is, and where an
  // object shows on the screen.

  private static void serve(LocalServerSocket server) {
    while (true) {
      try (LocalSocket s = server.accept()) {
        BufferedReader in = new BufferedReader(new InputStreamReader(s.getInputStream(), StandardCharsets.UTF_8));
        OutputStream out = s.getOutputStream();
        String line;
        while ((line = in.readLine()) != null) {
          String reply;
          try {
            reply = command(line);
          } catch (Exception e) {
            reply = "error " + e;
          }
          out.write((reply + "\n").getBytes(StandardCharsets.UTF_8));
          out.flush();
        }
      } catch (Exception e) {
        Log.w(TAG, "control: " + e);
      }
    }
  }

  private static String command(String line) throws Exception {
    if (line.startsWith("pose ")) {
      pose = floats(line.substring(5));
      return "ok";
    }
    if (line.startsWith("where ")) {
      String name = line.substring(6);
      int o = objectNames.indexOf(name);
      if (o < 0) return "none the scene has no object " + name + ": its objects are " + objectNames;
      if (texture < 0 || picture == null) return "none the app shows no AR camera view";
      float[] v = toView(objectTaps.get(o));
      if (v == null) return "none " + name + " is behind the camera";
      if (v[0] < 0 || v[1] < 0 || v[0] > viewW || v[1] > viewH) return "none " + name + " is out of the camera view";
      int[] at = viewOnScreen();
      if (at == null) return "none the AR camera view is not on the screen";
      return "at " + (at[0] + v[0]) + " " + (at[1] + v[1]);
    }
    return "error unknown command " + line;
  }

  /** Where the app's AR view is on the screen: the surface view of the size the app gave ARCore. */
  private static int[] viewOnScreen() throws Exception {
    Class<?> wmg = Class.forName("android.view.WindowManagerGlobal");
    Object global = wmg.getMethod("getInstance").invoke(null);
    Field views = wmg.getDeclaredField("mViews");
    views.setAccessible(true);
    @SuppressWarnings("unchecked")
    List<View> roots = new ArrayList<>((List<View>) views.get(global));
    for (View root : roots) {
      View found = find(root);
      if (found != null) {
        int[] at = new int[2];
        found.getLocationOnScreen(at);
        return at;
      }
    }
    return null;
  }

  private static View find(View v) {
    if (v instanceof SurfaceView && v.getWidth() == viewW && v.getHeight() == viewH && v.isShown()) return v;
    if (v instanceof ViewGroup) {
      ViewGroup g = (ViewGroup) v;
      for (int i = 0; i < g.getChildCount(); i++) {
        View f = find(g.getChildAt(i));
        if (f != null) return f;
      }
    }
    return null;
  }

  // ---- the camera's picture: the emulator's back camera, into the texture ARCore was given.

  private static void startPicture() throws Exception {
    picture = new SurfaceTexture(texture);
    picture.setDefaultBufferSize((int) SENSOR_W, (int) SENSOR_H);
    pictureSurface = new Surface(picture);
    CameraManager cm = context.getSystemService(CameraManager.class);
    String back = null;
    for (String id : cm.getCameraIdList()) {
      Integer facing = cm.getCameraCharacteristics(id).get(CameraCharacteristics.LENS_FACING);
      if (facing != null && facing == CameraCharacteristics.LENS_FACING_BACK) back = id;
    }
    if (back == null) throw new IllegalStateException("the emulator has no back camera");
    HandlerThread t = new HandlerThread("axx-ar-camera");
    t.start();
    cameraHandler = new Handler(t.getLooper());
    cm.openCamera(back, new CameraDevice.StateCallback() {
      @Override
      public void onOpened(CameraDevice d) {
        cameraDevice = d;
        try {
          d.createCaptureSession(Collections.singletonList(pictureSurface), new CameraCaptureSession.StateCallback() {
            @Override
            public void onConfigured(CameraCaptureSession s) {
              try {
                CaptureRequest.Builder b = d.createCaptureRequest(CameraDevice.TEMPLATE_PREVIEW);
                b.addTarget(pictureSurface);
                s.setRepeatingRequest(b.build(), null, cameraHandler);
              } catch (Exception e) {
                Log.e(TAG, "the camera's picture: " + e);
              }
            }

            @Override
            public void onConfigureFailed(CameraCaptureSession s) {
              Log.e(TAG, "the camera's picture: its session failed");
            }
          }, cameraHandler);
        } catch (Exception e) {
          Log.e(TAG, "the camera's picture: " + e);
        }
      }

      @Override
      public void onDisconnected(CameraDevice d) {
        d.close();
      }

      @Override
      public void onError(CameraDevice d, int error) {
        Log.e(TAG, "the camera failed: " + error);
        d.close();
      }
    }, cameraHandler);
  }

  private static void stopPicture() {
    if (cameraDevice != null) cameraDevice.close();
    cameraDevice = null;
    if (cameraHandler != null) cameraHandler.getLooper().quitSafely();
    cameraHandler = null;
    if (pictureSurface != null) pictureSurface.release();
    pictureSurface = null;
    if (picture != null) picture.release();
    picture = null;
    timestamp = 0;
  }

  // ---- ARCore's calls

  public static Object call(String owner, String name, String sig, Object self, Object[] args) throws Exception {
    if (loader == null) loader = (self instanceof Class ? (Class<?>) self : self.getClass()).getClassLoader();
    if (name.equals("equals") && sig.equals("(Ljava/lang/Object;)Z")) return self == args[0];
    if (name.equals("hashCode") && sig.equals("()I")) return System.identityHashCode(self);
    switch (owner) {
      case "ArCoreApk":
        return arCoreApk(name, args);
      case "Session":
        return session(name, sig, self, args);
      case "Config":
        return config(name, sig, self, args);
      case "Frame":
        return frame(name, sig, args);
      case "Camera":
        return camera(name, args);
      case "HitResult":
        return hitResult(name, self);
      case "Plane":
      case "Point":
      case "TrackableBase":
        return trackable(name, self, args);
      case "Anchor":
        return anchor(name, self);
      case "LightEstimate":
        return light(name, args);
      case "CameraIntrinsics":
        return intrinsics(name);
      default:
        return PROCEED;
    }
  }

  private static Object arCoreApk(String name, Object[] args) throws Exception {
    if (name.equals("checkAvailability")) return enumOf("ArCoreApk$Availability", "SUPPORTED_INSTALLED");
    if (name.equals("checkAvailabilityAsync")) {
      Object consumer = args[1];
      consumer.getClass().getMethod("accept", Object.class).invoke(consumer, enumOf("ArCoreApk$Availability", "SUPPORTED_INSTALLED"));
      return null;
    }
    if (name.equals("requestInstall")) return enumOf("ArCoreApk$InstallStatus", "INSTALLED");
    return PROCEED;
  }

  private static Object session(String name, String sig, Object self, Object[] args) throws Exception {
    switch (name) {
      case "<init>":
        context = ((Context) args[0]).getApplicationContext();
        frame = allocate(arClass("Frame"));
        camera = allocate(arClass("Camera"));
        lightEstimate = allocate(arClass("LightEstimate"));
        return null;
      case "createForSharedCamera":
        Object s = allocate(arClass("Session"));
        session("<init>", "(Landroid/content/Context;)V", s, args);
        return s;
      case "configure":
        config = args[0];
        return null;
      case "getConfig":
        if (sig.startsWith("()")) {
          if (config == null) config = newConfig();
          return config;
        }
        return null;
      case "isSupported":
        return true;
      case "resume":
        return null;
      case "pause":
      case "close":
        stopPicture();
        return null;
      case "setCameraTextureName":
        if ((Integer) args[0] != texture) stopPicture();
        texture = (Integer) args[0];
        return null;
      case "setCameraTextureNames":
        int[] names = (int[]) args[0];
        if (names.length > 0 && names[0] != texture) stopPicture();
        if (names.length > 0) texture = names[0];
        return null;
      case "setDisplayGeometry":
        rotation = (Integer) args[0];
        viewW = Math.max(1, (Integer) args[1]);
        viewH = Math.max(1, (Integer) args[2]);
        geometryChanged = true;
        return null;
      case "update":
        if (picture == null && texture >= 0) startPicture();
        if (picture != null) {
          picture.updateTexImage();
          picture.getTransformMatrix(textureMatrix);
          timestamp = picture.getTimestamp();
        }
        frameGeometryChanged = geometryChanged;
        geometryChanged = false;
        return frame;
      case "getAllAnchors":
        return new ArrayList<>(anchors.keySet());
      case "getAllTrackables":
        return new ArrayList<>();
      case "createAnchor":
        return newAnchor(poseValues(args[0]));
      case "getSupportedCameraConfigs":
        return new ArrayList<>();
      case "isDepthModeSupported":
      case "isSemanticModeSupported":
      case "isImageStabilizationModeSupported":
      case "isGeospatialModeSupported":
        return false;
      default:
        return defaultFor(sig);
    }
  }

  private static Object newConfig() throws Exception {
    Object c = allocate(arClass("Config"));
    configs.put(c, new HashMap<>());
    return c;
  }

  private static Object config(String name, String sig, Object self, Object[] args) throws Exception {
    Map<String, Object> values = configs.get(self);
    if (values == null) {
      values = new HashMap<>();
      configs.put(self, values);
    }
    if (name.equals("<init>")) return null;
    if (name.startsWith("set")) {
      values.put(name.substring(3), args[0]);
      return self;
    }
    if (name.startsWith("get")) {
      String key = name.substring(3);
      if (values.containsKey(key)) return values.get(key);
      String type = sig.substring(sig.indexOf(')') + 2, sig.length() - 1).replace('/', '.');
      if (!type.startsWith("com.google.ar.core.Config$")) return null;
      Class<?> e = Class.forName(type, true, loader);
      Object[] constants = e.getEnumConstants();
      for (Object c : constants)
        if (((Enum<?>) c).name().equals(key.equals("PlaneFindingMode") ? "HORIZONTAL" : key.equals("LightEstimationMode") ? "AMBIENT_INTENSITY" : "DISABLED"))
          return c;
      return constants.length > 0 ? constants[0] : null;
    }
    return defaultFor(sig);
  }

  private static Object frame(String name, String sig, Object[] args) throws Exception {
    switch (name) {
      case "getCamera":
        return camera;
      case "hasDisplayGeometryChanged":
        return frameGeometryChanged;
      case "getTimestamp":
      case "getAndroidCameraTimestamp":
        return timestamp;
      case "getAndroidSensorPose":
        return newPose(pose);
      case "getLightEstimate":
        return lightEstimate;
      case "getCameraTextureName":
        return texture;
      case "hitTest":
        if (sig.equals("(FF)Ljava/util/List;")) return hitTest((Float) args[0], (Float) args[1]);
        if (sig.startsWith("(Landroid/view/MotionEvent;)")) {
          MotionEvent e = (MotionEvent) args[0];
          return hitTest(e.getX(), e.getY());
        }
        if (sig.equals("([FI[FI)Ljava/util/List;")) {
          float[] o = (float[]) args[0], d = (float[]) args[2];
          int oi = (Integer) args[1], di = (Integer) args[3];
          return hitsOf(new float[] {o[oi], o[oi + 1], o[oi + 2]}, new float[] {d[di], d[di + 1], d[di + 2]});
        }
        return new ArrayList<>();
      case "hitTestInstantPlacement":
        return hitTest((Float) args[0], (Float) args[1]);
      case "getUpdatedAnchors":
        return new ArrayList<>(anchors.keySet());
      case "getUpdatedTrackables":
        return new ArrayList<>();
      case "transformCoordinates2d":
        transform2d(String.valueOf(args[0]), args[1], String.valueOf(args[2]), args[3]);
        return null;
      case "transformDisplayUvCoords":
        transform2d("VIEW_NORMALIZED", args[0], "TEXTURE_NORMALIZED", args[1]);
        return null;
      default:
        return defaultFor(sig);
    }
  }

  private static Object camera(String name, Object[] args) throws Exception {
    switch (name) {
      case "getTrackingState":
        return enumOf("TrackingState", "TRACKING");
      case "getTrackingFailureReason":
        return enumOf("TrackingFailureReason", "NONE");
      case "getPose":
        // The sensor's own axes: a quarter turn from the phone held upright.
        return newPose(multiply(pose, rotationZ(-90)));
      case "getDisplayOrientedPose":
        return newPose(pose);
      case "getViewMatrix":
        System.arraycopy(invertRigid(pose), 0, (float[]) args[0], (Integer) args[1], 16);
        return null;
      case "getProjectionMatrix":
        System.arraycopy(projection((Float) args[2], (Float) args[3]), 0, (float[]) args[0], (Integer) args[1], 16);
        return null;
      case "getImageIntrinsics":
      case "getTextureIntrinsics":
        return allocate(arClass("CameraIntrinsics"));
      default:
        return null;
    }
  }

  private static Object intrinsics(String name) {
    switch (name) {
      case "getFocalLength":
        return new float[] {FOCAL, FOCAL};
      case "getPrincipalPoint":
        return new float[] {SENSOR_W / 2, SENSOR_H / 2};
      case "getImageDimensions":
        return new int[] {(int) SENSOR_W, (int) SENSOR_H};
      default:
        return null;
    }
  }

  private static Object hitResult(String name, Object self) throws Exception {
    Hit h = hits.get(self);
    if (h == null) return null;
    switch (name) {
      case "getHitPose":
        return newPose(hitPose(h));
      case "getDistance":
        return h.distance;
      case "getTrackable":
        return h.plane;
      case "createAnchor":
        return newAnchor(hitPose(h));
      default:
        return null;
    }
  }

  private static Object trackable(String name, Object self, Object[] args) throws Exception {
    float[] p = planes.get(self);
    switch (name) {
      case "getTrackingState":
        return enumOf("TrackingState", "TRACKING");
      case "getType":
        float ny = p == null ? 1 : p[16 + 1];
        return enumOf("Plane$Type", ny > 0.7f ? "HORIZONTAL_UPWARD_FACING" : ny < -0.7f ? "HORIZONTAL_DOWNWARD_FACING" : "VERTICAL");
      case "getCenterPose":
      case "getPose":
        return newPose(p == null ? identity() : Arrays.copyOf(p, 16));
      case "getExtentX":
      case "getExtentZ":
        return 1f;
      case "getPolygon":
        return ByteBuffer.allocateDirect(32).order(ByteOrder.nativeOrder()).asFloatBuffer().put(new float[] {-0.5f, -0.5f, 0.5f, -0.5f, 0.5f, 0.5f, -0.5f, 0.5f}).rewind();
      case "isPoseInPolygon":
      case "isPoseInExtents":
        return true;
      case "getSubsumedBy":
        return null;
      case "getAnchors":
        return new ArrayList<>();
      case "createAnchor":
        return newAnchor(poseValues(args[0]));
      case "getOrientationMode":
        return enumOf("Point$OrientationMode", "ESTIMATED_SURFACE_NORMAL");
      default:
        return null;
    }
  }

  private static Object anchor(String name, Object self) throws Exception {
    switch (name) {
      case "getPose":
        float[] m = anchors.get(self);
        return newPose(m == null ? identity() : m);
      case "getTrackingState":
        return enumOf("TrackingState", anchors.containsKey(self) ? "TRACKING" : "STOPPED");
      case "detach":
        anchors.remove(self);
        return null;
      case "getCloudAnchorState":
        return enumOf("Anchor$CloudAnchorState", "NONE");
      case "getTerrainAnchorState":
        return enumOf("Anchor$TerrainAnchorState", "NONE");
      default:
        return null;
    }
  }

  private static Object light(String name, Object[] args) throws Exception {
    switch (name) {
      case "getState":
        return enumOf("LightEstimate$State", "NOT_VALID");
      case "getPixelIntensity":
        return 1f;
      case "getColorCorrection":
        float[] out = (float[]) args[0];
        int at = (Integer) args[1];
        for (int i = 0; i < 4; i++) out[at + i] = 1;
        return null;
      case "getTimestamp":
        return timestamp;
      default:
        return null;
    }
  }

  private static Object defaultFor(String sig) {
    switch (sig.charAt(sig.indexOf(')') + 1)) {
      case 'Z':
        return false;
      case 'I':
      case 'S':
      case 'B':
      case 'C':
        return 0;
      case 'J':
        return 0L;
      case 'F':
        return 0f;
      case 'D':
        return 0d;
      default:
        return null;
    }
  }

  // ---- hit tests

  private static List<Object> hitTest(float x, float y) throws Exception {
    float[] upright = viewToUpright(x / viewW, y / viewH);
    float[] dirCamera = {(upright[0] * UPRIGHT_W - UPRIGHT_W / 2) / FOCAL, -(upright[1] * UPRIGHT_H - UPRIGHT_H / 2) / FOCAL, -1};
    float[] m = pose;
    float[] dir = {
      m[0] * dirCamera[0] + m[4] * dirCamera[1] + m[8] * dirCamera[2],
      m[1] * dirCamera[0] + m[5] * dirCamera[1] + m[9] * dirCamera[2],
      m[2] * dirCamera[0] + m[6] * dirCamera[1] + m[10] * dirCamera[2],
    };
    return hitsOf(new float[] {m[12], m[13], m[14]}, dir);
  }

  /** The ray's nearest hit on the scene: a named object's where a tap on it lands. */
  private static List<Object> hitsOf(float[] o, float[] d) throws Exception {
    float len = (float) Math.sqrt(d[0] * d[0] + d[1] * d[1] + d[2] * d[2]);
    float[] dir = {d[0] / len, d[1] / len, d[2] / len};
    float best = Float.MAX_VALUE;
    int bestTri = -1;
    for (int t = 0; t * 9 + 8 < triangles.length; t++) {
      float hit = intersect(o, dir, t);
      if (hit > 1e-4f && hit < best) {
        best = hit;
        bestTri = t;
      }
    }
    List<Object> out = new ArrayList<>();
    if (bestTri < 0) return out;
    float[] normal = normalOf(bestTri, dir);
    float[] point = {o[0] + dir[0] * best, o[1] + dir[1] * best, o[2] + dir[2] * best};
    int object = triangleObject[bestTri];
    if (object >= 0) point = objectTaps.get(object).clone();
    float distance = (float) Math.sqrt(sq(point[0] - o[0]) + sq(point[1] - o[1]) + sq(point[2] - o[2]));
    Object plane = allocate(arClass("Plane"));
    float[] planeData = Arrays.copyOf(poseAt(point, normal), 19);
    planeData[16] = normal[0];
    planeData[17] = normal[1];
    planeData[18] = normal[2];
    planes.put(plane, planeData);
    Object result = allocate(arClass("HitResult"));
    hits.put(result, new Hit(point, normal, distance, plane));
    out.add(result);
    Log.i(TAG, "hit " + (object >= 0 ? objectNames.get(object) + " (where a tap on it lands)" : "the scene") + " at " + point[0] + " " + point[1] + " " + point[2]);
    return out;
  }

  private static float intersect(float[] o, float[] d, int t) {
    int b = t * 9;
    float[] v = triangles;
    float e1x = v[b + 3] - v[b], e1y = v[b + 4] - v[b + 1], e1z = v[b + 5] - v[b + 2];
    float e2x = v[b + 6] - v[b], e2y = v[b + 7] - v[b + 1], e2z = v[b + 8] - v[b + 2];
    float px = d[1] * e2z - d[2] * e2y, py = d[2] * e2x - d[0] * e2z, pz = d[0] * e2y - d[1] * e2x;
    float det = e1x * px + e1y * py + e1z * pz;
    if (Math.abs(det) < 1e-9f) return -1;
    float inv = 1 / det;
    float tx = o[0] - v[b], ty = o[1] - v[b + 1], tz = o[2] - v[b + 2];
    float u = (tx * px + ty * py + tz * pz) * inv;
    if (u < 0 || u > 1) return -1;
    float qx = ty * e1z - tz * e1y, qy = tz * e1x - tx * e1z, qz = tx * e1y - ty * e1x;
    float w = (d[0] * qx + d[1] * qy + d[2] * qz) * inv;
    if (w < 0 || u + w > 1) return -1;
    return (e2x * qx + e2y * qy + e2z * qz) * inv;
  }

  /** The triangle's normal, facing the ray's origin. */
  private static float[] normalOf(int t, float[] d) {
    int b = t * 9;
    float[] v = triangles;
    float e1x = v[b + 3] - v[b], e1y = v[b + 4] - v[b + 1], e1z = v[b + 5] - v[b + 2];
    float e2x = v[b + 6] - v[b], e2y = v[b + 7] - v[b + 1], e2z = v[b + 8] - v[b + 2];
    float nx = e1y * e2z - e1z * e2y, ny = e1z * e2x - e1x * e2z, nz = e1x * e2y - e1y * e2x;
    float l = (float) Math.sqrt(nx * nx + ny * ny + nz * nz);
    if (l == 0) return new float[] {0, 1, 0};
    nx /= l;
    ny /= l;
    nz /= l;
    if (nx * d[0] + ny * d[1] + nz * d[2] > 0) return new float[] {-nx, -ny, -nz};
    return new float[] {nx, ny, nz};
  }

  /** A pose at the point whose +Y is the surface's normal, as ARCore's hits on planes are. */
  private static float[] poseAt(float[] p, float[] n) {
    float[] up = n;
    float[] ref = Math.abs(up[2]) < 0.9f ? new float[] {0, 0, 1} : new float[] {1, 0, 0};
    float[] x = cross(up, ref);
    normalize(x);
    float[] z = cross(x, up);
    return new float[] {x[0], x[1], x[2], 0, up[0], up[1], up[2], 0, z[0], z[1], z[2], 0, p[0], p[1], p[2], 1};
  }

  private static float[] hitPose(Hit h) {
    return poseAt(h.point, h.normal);
  }

  private static Object newAnchor(float[] m) throws Exception {
    Object a = allocate(arClass("Anchor"));
    anchors.put(a, m);
    return a;
  }

  // ---- the camera's geometry: the view, the picture held upright, the sensor's picture

  /** How much of the upright picture the view shows across and down: ARCore fills the view. */
  private static float[] shown() {
    float scale = Math.max(viewW / UPRIGHT_W, viewH / UPRIGHT_H);
    return new float[] {viewW / scale / UPRIGHT_W, viewH / scale / UPRIGHT_H};
  }

  private static float[] viewToUpright(float xn, float yn) {
    float[] f = shown();
    return new float[] {0.5f + (xn - 0.5f) * f[0], 0.5f + (yn - 0.5f) * f[1]};
  }

  private static float[] uprightToView(float u, float v) {
    float[] f = shown();
    return new float[] {0.5f + (u - 0.5f) / f[0], 0.5f + (v - 0.5f) / f[1]};
  }

  /** Where a point of the world shows in the view, in its pixels; null behind the camera. */
  private static float[] toView(float[] p) {
    float[] inv = invertRigid(pose);
    float cx = inv[0] * p[0] + inv[4] * p[1] + inv[8] * p[2] + inv[12];
    float cy = inv[1] * p[0] + inv[5] * p[1] + inv[9] * p[2] + inv[13];
    float cz = inv[2] * p[0] + inv[6] * p[1] + inv[10] * p[2] + inv[14];
    if (cz >= 0) return null;
    float u = (UPRIGHT_W / 2 + FOCAL * cx / -cz) / UPRIGHT_W;
    float v = (UPRIGHT_H / 2 - FOCAL * cy / -cz) / UPRIGHT_H;
    float[] n = uprightToView(u, v);
    return new float[] {n[0] * viewW, n[1] * viewH};
  }

  /** ARCore's projection for the display-oriented camera, as the picture fills the view. */
  private static float[] projection(float near, float far) {
    float[] f = shown();
    float w = f[0] * UPRIGHT_W, h = f[1] * UPRIGHT_H;
    float[] m = new float[16];
    m[0] = 2 * FOCAL / w;
    m[5] = 2 * FOCAL / h;
    m[10] = -(far + near) / (far - near);
    m[11] = -1;
    m[14] = -2 * far * near / (far - near);
    return m;
  }

  /**
   * Converts points between ARCore's 2D coordinates: the view (pixels, normalized, or OpenGL's
   * device coordinates), the camera's image (the sensor's picture, in pixels or normalized), and
   * the texture it is in (normalized, or texels).
   */
  private static void transform2d(String from, Object in, String to, Object out) {
    float[] src = in instanceof float[] ? (float[]) in : copyOf((FloatBuffer) in);
    float[] dst = new float[src.length];
    for (int i = 0; i + 1 < src.length; i += 2) {
      float[] u = toUpright(from, src[i], src[i + 1]);
      float[] r = fromUpright(to, u[0], u[1]);
      dst[i] = r[0];
      dst[i + 1] = r[1];
    }
    if (out instanceof float[]) System.arraycopy(dst, 0, (float[]) out, 0, Math.min(dst.length, ((float[]) out).length));
    else {
      FloatBuffer b = (FloatBuffer) out;
      int at = b.position();
      for (int i = 0; i < dst.length && at + i < b.limit(); i++) b.put(at + i, dst[i]);
    }
  }

  private static float[] copyOf(FloatBuffer b) {
    float[] out = new float[b.remaining()];
    for (int i = 0; i < out.length; i++) out[i] = b.get(b.position() + i);
    return out;
  }

  /** A point in the named coordinates, as a point of the picture held upright, normalized. */
  private static float[] toUpright(String kind, float x, float y) {
    switch (kind) {
      case "VIEW":
        return viewToUpright(x / viewW, y / viewH);
      case "VIEW_NORMALIZED":
        return viewToUpright(x, y);
      case "OPENGL_NORMALIZED_DEVICE_COORDINATES":
        return viewToUpright((x + 1) / 2, (1 - y) / 2);
      case "IMAGE_PIXELS":
        return imageToUpright(x / SENSOR_W, y / SENSOR_H);
      case "IMAGE_NORMALIZED":
        return imageToUpright(x, y);
      case "TEXTURE_TEXELS":
        return textureToUpright(x / SENSOR_W, y / SENSOR_H);
      default: // TEXTURE_NORMALIZED
        return textureToUpright(x, y);
    }
  }

  private static float[] fromUpright(String kind, float u, float v) {
    switch (kind) {
      case "VIEW": {
        float[] n = uprightToView(u, v);
        return new float[] {n[0] * viewW, n[1] * viewH};
      }
      case "VIEW_NORMALIZED":
        return uprightToView(u, v);
      case "OPENGL_NORMALIZED_DEVICE_COORDINATES": {
        float[] n = uprightToView(u, v);
        return new float[] {n[0] * 2 - 1, 1 - n[1] * 2};
      }
      case "IMAGE_PIXELS": {
        float[] s = uprightToImage(u, v);
        return new float[] {s[0] * SENSOR_W, s[1] * SENSOR_H};
      }
      case "IMAGE_NORMALIZED":
        return uprightToImage(u, v);
      case "TEXTURE_TEXELS": {
        float[] t = uprightToTexture(u, v);
        return new float[] {t[0] * SENSOR_W, t[1] * SENSOR_H};
      }
      default: // TEXTURE_NORMALIZED
        return uprightToTexture(u, v);
    }
  }

  // The sensor's picture turned a quarter clockwise is the picture held upright: its x down the
  // upright picture, its y from its right to its left.
  private static float[] uprightToImage(float u, float v) {
    return new float[] {v, 1 - u};
  }

  private static float[] imageToUpright(float s, float t) {
    return new float[] {1 - t, s};
  }

  // The texture holds the sensor's picture as the camera streams it, its rows as OpenGL's go,
  // read through the stream's transform.
  private static float[] uprightToTexture(float u, float v) {
    float[] s = uprightToImage(u, v);
    float x = s[0], y = 1 - s[1];
    float[] m = textureMatrix;
    return new float[] {m[0] * x + m[4] * y + m[12], m[1] * x + m[5] * y + m[13]};
  }

  private static float[] textureToUpright(float tx, float ty) {
    float[] m = textureMatrix;
    float det = m[0] * m[5] - m[4] * m[1];
    float x = tx - m[12], y = ty - m[13];
    float sx = (m[5] * x - m[4] * y) / det, sy = (-m[1] * x + m[0] * y) / det;
    return imageToUpright(sx, 1 - sy);
  }

  // ---- ARCore's types, through the app's own classes

  private static Class<?> arClass(String name) throws Exception {
    Class<?> c = classes.get(name);
    if (c == null) {
      c = Class.forName("com.google.ar.core." + name, true, loader);
      classes.put(name, c);
    }
    return c;
  }

  @SuppressWarnings({"unchecked", "rawtypes"})
  private static Object enumOf(String type, String name) throws Exception {
    return Enum.valueOf((Class) arClass(type), name);
  }

  private static Object newPose(float[] m) throws Exception {
    float[] q = quaternion(m);
    return arClass("Pose").getConstructor(float[].class, float[].class).newInstance(new float[] {m[12], m[13], m[14]}, q);
  }

  private static float[] poseValues(Object p) throws Exception {
    Method toMatrix = arClass("Pose").getMethod("toMatrix", float[].class, int.class);
    float[] m = new float[16];
    toMatrix.invoke(p, m, 0);
    return m;
  }

  // ---- matrices, column by column

  private static float[] identity() {
    return new float[] {1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1};
  }

  private static float[] matrix(JSONArray a) throws Exception {
    float[] m = new float[16];
    for (int i = 0; i < 16; i++) m[i] = (float) a.getDouble(i);
    return m;
  }

  private static float[] floats(String s) {
    String[] parts = s.trim().split("\\s+");
    float[] m = new float[16];
    for (int i = 0; i < 16 && i < parts.length; i++) m[i] = Float.parseFloat(parts[i]);
    return m;
  }

  private static float[] multiply(float[] a, float[] b) {
    float[] out = new float[16];
    for (int c = 0; c < 4; c++)
      for (int r = 0; r < 4; r++) {
        float s = 0;
        for (int k = 0; k < 4; k++) s += a[k * 4 + r] * b[c * 4 + k];
        out[c * 4 + r] = s;
      }
    return out;
  }

  private static float[] transform(float[] m, float x, float y, float z) {
    return new float[] {
      m[0] * x + m[4] * y + m[8] * z + m[12], m[1] * x + m[5] * y + m[9] * z + m[13], m[2] * x + m[6] * y + m[10] * z + m[14],
    };
  }

  /** The inverse of a rotation and a translation. */
  private static float[] invertRigid(float[] m) {
    float[] out = new float[16];
    for (int r = 0; r < 3; r++)
      for (int c = 0; c < 3; c++) out[c * 4 + r] = m[r * 4 + c];
    for (int r = 0; r < 3; r++) out[12 + r] = -(out[r] * m[12] + out[4 + r] * m[13] + out[8 + r] * m[14]);
    out[15] = 1;
    return out;
  }

  private static float[] rotationZ(float degrees) {
    double a = Math.toRadians(degrees);
    float c = (float) Math.cos(a), s = (float) Math.sin(a);
    return new float[] {c, s, 0, 0, -s, c, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1};
  }

  /** The rotation of a transform as a quaternion: x, y, z, w. */
  private static float[] quaternion(float[] m) {
    float[] col = new float[9];
    for (int c = 0; c < 3; c++) {
      float l = (float) Math.sqrt(sq(m[c * 4]) + sq(m[c * 4 + 1]) + sq(m[c * 4 + 2]));
      if (l == 0) l = 1;
      for (int r = 0; r < 3; r++) col[c * 3 + r] = m[c * 4 + r] / l;
    }
    float r00 = col[0], r11 = col[4], r22 = col[8], r10 = col[1], r01 = col[3], r20 = col[2], r02 = col[6], r21 = col[5], r12 = col[7];
    float x, y, z, w, tr = r00 + r11 + r22;
    if (tr > 0) {
      float s = (float) Math.sqrt(tr + 1) * 2;
      w = s / 4;
      x = (r21 - r12) / s;
      y = (r02 - r20) / s;
      z = (r10 - r01) / s;
    } else if (r00 > r11 && r00 > r22) {
      float s = (float) Math.sqrt(1 + r00 - r11 - r22) * 2;
      w = (r21 - r12) / s;
      x = s / 4;
      y = (r01 + r10) / s;
      z = (r02 + r20) / s;
    } else if (r11 > r22) {
      float s = (float) Math.sqrt(1 + r11 - r00 - r22) * 2;
      w = (r02 - r20) / s;
      x = (r01 + r10) / s;
      y = s / 4;
      z = (r12 + r21) / s;
    } else {
      float s = (float) Math.sqrt(1 + r22 - r00 - r11) * 2;
      w = (r10 - r01) / s;
      x = (r02 + r20) / s;
      y = (r12 + r21) / s;
      z = s / 4;
    }
    return new float[] {x, y, z, w};
  }

  private static float[] cross(float[] a, float[] b) {
    return new float[] {a[1] * b[2] - a[2] * b[1], a[2] * b[0] - a[0] * b[2], a[0] * b[1] - a[1] * b[0]};
  }

  private static void normalize(float[] v) {
    float l = (float) Math.sqrt(v[0] * v[0] + v[1] * v[1] + v[2] * v[2]);
    if (l > 0) {
      v[0] /= l;
      v[1] /= l;
      v[2] /= l;
    }
  }

  private static float sq(float v) {
    return v * v;
  }
}
