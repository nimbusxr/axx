// SPDX-License-Identifier: Apache-2.0

package example.parcels.courier

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.LaunchedEffect
import androidx.core.content.ContextCompat
import android.opengl.GLES11Ext
import android.opengl.GLES20
import android.opengl.GLSurfaceView
import android.opengl.Matrix
import android.view.MotionEvent
import android.view.Surface
import android.view.WindowManager
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import com.google.ar.core.ArCoreApk
import com.google.ar.core.Config
import com.google.ar.core.Coordinates2d
import com.google.ar.core.Frame
import com.google.ar.core.Session
import com.google.ar.core.TrackingState
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.nio.FloatBuffer
import java.util.concurrent.ConcurrentLinkedQueue
import javax.microedition.khronos.egl.EGLConfig
import javax.microedition.khronos.opengles.GL10
import kotlin.math.roundToInt
import kotlin.math.sqrt

/**
 * The courier measures a parcel with the phone's camera, to see that it fits a parcel locker: a
 * tap on one end of the parcel, then on the other, and the app shows how far apart they are, as
 * ARCore finds them.
 */
@Composable
fun MeasureParcelScreen(onDone: () -> Unit) {
    val context = LocalContext.current
    val supported = remember {
        runCatching { ArCoreApk.getInstance().checkAvailability(context) == ArCoreApk.Availability.SUPPORTED_INSTALLED }.getOrDefault(false)
    }
    var meters by remember { mutableStateOf<Double?>(null) }
    var allowed by remember {
        mutableStateOf(ContextCompat.checkSelfPermission(context, Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED)
    }
    val ask = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { allowed = it }
    LaunchedEffect(Unit) { if (!allowed) ask.launch(Manifest.permission.CAMERA) }
    Column(Modifier.fillMaxSize()) {
        Row(Modifier.fillMaxWidth().padding(start = 24.dp, end = 12.dp, top = 12.dp), verticalAlignment = Alignment.CenterVertically) {
            Text("Measure parcel", style = MaterialTheme.typography.titleLarge, modifier = Modifier.weight(1f))
            TextButton(onClick = onDone) { Text("Done") }
        }
        if (!supported) {
            Text("This phone cannot measure with its camera.", Modifier.padding(24.dp))
            return@Column
        }
        if (!allowed) return@Column
        val measure = remember { ParcelCamera(context) { meters = it } }
        DisposableEffect(measure) { onDispose { measure.close() } }
        Box(Modifier.fillMaxSize().background(Color.Black)) {
            AndroidView(modifier = Modifier.fillMaxSize(), factory = { measure.view })
            Text(
                meters?.let { "${(it * 100).roundToInt()} cm" } ?: "Tap one end of the parcel, then the other",
                color = Color.White,
                style = if (meters == null) MaterialTheme.typography.bodyMedium else MaterialTheme.typography.headlineLarge,
                modifier = Modifier
                    .align(Alignment.BottomCenter)
                    .padding(bottom = 40.dp)
                    .background(Color.Black.copy(alpha = 0.6f), RoundedCornerShape(12.dp))
                    .padding(horizontal = 16.dp, vertical = 8.dp)
                    .semantics { liveRegion = LiveRegionMode.Polite },
            )
        }
    }
}

/**
 * ARCore's session, shown in a GL view: the camera's picture, and a mark on each point the
 * courier taps, where ARCore finds it in the world.
 */
private class ParcelCamera(context: Context, private val measured: (Double?) -> Unit) : GLSurfaceView.Renderer {
    private val session = Session(context).apply {
        configure(Config(this).apply { planeFindingMode = Config.PlaneFindingMode.HORIZONTAL_AND_VERTICAL })
        resume()
    }
    private val display = context.getSystemService(WindowManager::class.java).defaultDisplay
    private val taps = ConcurrentLinkedQueue<Pair<Float, Float>>()
    private val points = mutableListOf<FloatArray>()
    private val background = Background()
    private val marks = Marks()

    val view = GLSurfaceView(context).apply {
        preserveEGLContextOnPause = true
        setEGLContextClientVersion(2)
        setEGLConfigChooser(8, 8, 8, 8, 16, 0)
        setRenderer(this@ParcelCamera)
        renderMode = GLSurfaceView.RENDERMODE_CONTINUOUSLY
        setOnTouchListener { v, e ->
            if (e.action == MotionEvent.ACTION_UP) {
                taps.add(e.x to e.y)
                v.performClick()
            }
            true
        }
    }

    fun close() {
        view.onPause()
        session.pause()
        session.close()
    }

    override fun onSurfaceCreated(gl: GL10?, config: EGLConfig?) {
        GLES20.glClearColor(0f, 0f, 0f, 1f)
        background.create()
        marks.create()
        session.setCameraTextureName(background.texture)
    }

    override fun onSurfaceChanged(gl: GL10?, width: Int, height: Int) {
        GLES20.glViewport(0, 0, width, height)
        session.setDisplayGeometry(display?.rotation ?: Surface.ROTATION_0, width, height)
    }

    override fun onDrawFrame(gl: GL10?) {
        GLES20.glClear(GLES20.GL_COLOR_BUFFER_BIT or GLES20.GL_DEPTH_BUFFER_BIT)
        val frame = runCatching { session.update() }.getOrNull() ?: return
        background.draw(frame)
        val camera = frame.camera
        if (camera.trackingState != TrackingState.TRACKING) return
        while (true) {
            val (x, y) = taps.poll() ?: break
            val hit = frame.hitTest(x, y).firstOrNull() ?: continue
            if (points.size == 2) {
                points.clear()
                measured(null)
            }
            points.add(hit.hitPose.translation)
            if (points.size == 2) {
                val (a, b) = points
                val d = FloatArray(3) { a[it] - b[it] }
                val meters = sqrt((d[0] * d[0] + d[1] * d[1] + d[2] * d[2]).toDouble())
                view.post { measured(meters) }
            }
        }
        val projection = FloatArray(16)
        val viewMatrix = FloatArray(16)
        camera.getProjectionMatrix(projection, 0, 0.05f, 100f)
        camera.getViewMatrix(viewMatrix, 0)
        val viewProjection = FloatArray(16)
        Matrix.multiplyMM(viewProjection, 0, projection, 0, viewMatrix, 0)
        marks.draw(points, viewProjection)
    }
}

private fun floats(vararg values: Float): FloatBuffer =
    ByteBuffer.allocateDirect(values.size * 4).order(ByteOrder.nativeOrder()).asFloatBuffer().apply {
        put(values)
        position(0)
    }

private fun program(vertex: String, fragment: String): Int {
    fun shader(type: Int, source: String) = GLES20.glCreateShader(type).also {
        GLES20.glShaderSource(it, source)
        GLES20.glCompileShader(it)
    }
    return GLES20.glCreateProgram().also {
        GLES20.glAttachShader(it, shader(GLES20.GL_VERTEX_SHADER, vertex))
        GLES20.glAttachShader(it, shader(GLES20.GL_FRAGMENT_SHADER, fragment))
        GLES20.glLinkProgram(it)
    }
}

/** The camera's picture, behind everything: ARCore's texture of it, fitted to the view as ARCore says. */
private class Background {
    var texture = 0
        private set
    private var program = 0
    private val corners = floats(-1f, -1f, 1f, -1f, -1f, 1f, 1f, 1f)
    private val uvs = floats(0f, 0f, 0f, 0f, 0f, 0f, 0f, 0f)

    fun create() {
        val names = IntArray(1)
        GLES20.glGenTextures(1, names, 0)
        texture = names[0]
        GLES20.glBindTexture(GLES11Ext.GL_TEXTURE_EXTERNAL_OES, texture)
        GLES20.glTexParameteri(GLES11Ext.GL_TEXTURE_EXTERNAL_OES, GLES20.GL_TEXTURE_MIN_FILTER, GLES20.GL_LINEAR)
        GLES20.glTexParameteri(GLES11Ext.GL_TEXTURE_EXTERNAL_OES, GLES20.GL_TEXTURE_MAG_FILTER, GLES20.GL_LINEAR)
        program = program(
            "attribute vec2 corner; attribute vec2 uv; varying vec2 v; void main() { v = uv; gl_Position = vec4(corner, 0.0, 1.0); }",
            "#extension GL_OES_EGL_image_external : require\nprecision mediump float; varying vec2 v; uniform samplerExternalOES picture; " +
                "void main() { gl_FragColor = texture2D(picture, v); }",
        )
    }

    fun draw(frame: Frame) {
        if (frame.hasDisplayGeometryChanged()) {
            frame.transformCoordinates2d(Coordinates2d.OPENGL_NORMALIZED_DEVICE_COORDINATES, corners, Coordinates2d.TEXTURE_NORMALIZED, uvs)
        }
        if (frame.timestamp == 0L) return
        GLES20.glDisable(GLES20.GL_DEPTH_TEST)
        GLES20.glUseProgram(program)
        GLES20.glActiveTexture(GLES20.GL_TEXTURE0)
        GLES20.glBindTexture(GLES11Ext.GL_TEXTURE_EXTERNAL_OES, texture)
        val corner = GLES20.glGetAttribLocation(program, "corner")
        val uv = GLES20.glGetAttribLocation(program, "uv")
        GLES20.glVertexAttribPointer(corner, 2, GLES20.GL_FLOAT, false, 0, corners)
        GLES20.glVertexAttribPointer(uv, 2, GLES20.GL_FLOAT, false, 0, uvs)
        GLES20.glEnableVertexAttribArray(corner)
        GLES20.glEnableVertexAttribArray(uv)
        GLES20.glDrawArrays(GLES20.GL_TRIANGLE_STRIP, 0, 4)
        GLES20.glDisableVertexAttribArray(corner)
        GLES20.glDisableVertexAttribArray(uv)
    }
}

/** A yellow dot on each point the courier tapped, where it is in the world. */
private class Marks {
    private var program = 0

    fun create() {
        program = program(
            "attribute vec4 point; uniform mat4 viewProjection; void main() { gl_Position = viewProjection * point; gl_PointSize = 28.0; }",
            "precision mediump float; void main() { if (length(gl_PointCoord - vec2(0.5)) > 0.5) discard; gl_FragColor = vec4(1.0, 0.8, 0.0, 1.0); }",
        )
    }

    fun draw(points: List<FloatArray>, viewProjection: FloatArray) {
        if (points.isEmpty()) return
        val buffer = floats(*points.flatMap { listOf(it[0], it[1], it[2], 1f) }.toFloatArray())
        GLES20.glUseProgram(program)
        GLES20.glUniformMatrix4fv(GLES20.glGetUniformLocation(program, "viewProjection"), 1, false, viewProjection, 0)
        val point = GLES20.glGetAttribLocation(program, "point")
        GLES20.glVertexAttribPointer(point, 4, GLES20.GL_FLOAT, false, 0, buffer)
        GLES20.glEnableVertexAttribArray(point)
        GLES20.glDrawArrays(GLES20.GL_POINTS, 0, points.size)
        GLES20.glDisableVertexAttribArray(point)
    }
}
