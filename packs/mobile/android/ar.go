package mobileandroid

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/packs/mobile/internal/scene"
)

// An app's AR, from the scene its camera sees: ARCore needs Google's services for AR, which an
// emulator has not got, and would see the scene only as pictures. axx attaches an agent of its
// own to a debuggable app that has ARCore (ar/agent.cc), as the app starts, and the agent's
// bridge (ar/Bridge.java) answers the app's ARCore calls from the scene: its camera at the
// phone's pose, its picture the emulator's camera's, its hit tests on the scene's surfaces, and
// on a named object where a tap on it lands. axx tells the bridge where the phone is, and asks
// it where an object shows on the screen, through a socket of the bridge's, which adb forwards.

//go:embed ar/agent.cc ar/Bridge.java
var arSources embed.FS

// arcoreType is ARCore's session's class, as an app's dex files name it.
var arcoreType = []byte("Lcom/google/ar/core/Session;")

// hasARCore is whether the app's APK has ARCore: its dex files name its session's class.
func hasARCore(sc *core.Scenario, apk string) (bool, error) {
	fi, err := os.Stat(apk)
	if err != nil {
		return false, err
	}
	return core.Cached(sc.Suite(), fmt.Sprintf("%s/arcore/%s/%d/%d", Name, apk, fi.Size(), fi.ModTime().UnixNano()), func() (bool, error) {
		z, err := zip.OpenReader(apk)
		if err != nil {
			return false, err
		}
		defer z.Close()
		for _, f := range z.File {
			if !strings.HasPrefix(f.Name, "classes") || !strings.HasSuffix(f.Name, ".dex") {
				continue
			}
			r, err := f.Open()
			if err != nil {
				return false, err
			}
			b, err := io.ReadAll(r)
			_ = r.Close()
			if err != nil {
				return false, err
			}
			if bytes.Contains(b, arcoreType) {
				return true, nil
			}
		}
		return false, nil
	})
}

// usesAR is whether the app's launch attaches the AR agent: its camera sees a scene, on an
// emulator axx starts, and the app has ARCore.
func (d *running) usesAR() bool {
	c := d.app.camera
	if c == nil || c.scene == nil || d.dev == nil || d.dev.avd == "" || d.scenario == nil {
		return false
	}
	ok, err := hasARCore(d.scenario, d.app.apk)
	if err != nil {
		d.scenario.Log("cannot read the %s app's APK for ARCore: %v", d.app.name, err)
	}
	return ok
}

// prepareAR puts the AR agent in the app's files and has the launch attach it, before the agent
// another pack attaches (models-android's), which it attaches in turn.
func (d *running) prepareAR(ctx context.Context, l *Launch) error {
	cam := d.app.camera
	if l.DataDir == "" {
		// run-as refuses an app that is not debuggable: no agent can attach to it.
		if err := l.Prepare(); err != nil { //nolint:nilerr // the app runs, its ARCore as it is
			d.scenario.Log("the %s app is not debuggable (%v), so its ARCore is not played: it sees the camera's picture of the scene, as ARCore would without Google's services for AR", d.app.name, err)
			return nil
		}
	}
	native, _ := arSources.ReadFile("ar/agent.cc")
	java, _ := arSources.ReadFile("ar/Bridge.java")
	dir, err := BuildAgent(ctx, l.SDKDir, l.ABI, Agent{Name: "axx-ar", Native: native, Java: java, JavaFile: "Bridge.java"})
	if err != nil {
		return fmt.Errorf("cannot build axx's AR agent for the %s app: %w", d.app.name, err)
	}
	files, err := d.Files(ctx, "code_cache/axx-ar")
	if err != nil {
		return err
	}
	var sceneJSON bytes.Buffer
	if err := cam.scene.Encode(&sceneJSON); err != nil {
		return err
	}
	suffix := make([]byte, 6)
	_, _ = rand.Read(suffix)
	socket := "axx-ar-" + hex.EncodeToString(suffix)
	contents := map[string][]byte{"scene.json": sceneJSON.Bytes(), "socket": []byte(socket), "pose": []byte(poseLine(cam.Pose())), "status": nil}
	for _, name := range []string{"libaxx-ar.so", "classes.dex"} {
		if contents[name], err = os.ReadFile(filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	for name, body := range contents {
		if err := files.Write(ctx, name, body); err != nil {
			return err
		}
	}
	remote := l.DataDir + "/code_cache/axx-ar"
	agent := remote + "/libaxx-ar.so=" + remote
	if l.Agent != "" {
		// Not ';', which the device's shell takes for its own: an app's folder has no comma.
		agent += "," + l.Agent
	}
	l.Agent = agent
	l.After = append(l.After, func() error { return d.connectAR(ctx, files, socket) })
	return nil
}

// connectAR waits for the bridge to be ready, and reaches its socket.
func (d *running) connectAR(ctx context.Context, files core.Files, socket string) error {
	deadline := time.Now().Add(15 * time.Second)
	for {
		b, _, err := files.Read(ctx, "status")
		if err != nil {
			return err
		}
		if string(b) == "ready" {
			break
		}
		if strings.HasPrefix(string(b), "error:") {
			return fmt.Errorf("axx's AR agent in the %s app: %s", d.app.name, strings.TrimPrefix(string(b), "error: "))
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("axx's AR agent did not start in the %s app: Android refused it, or failed to load it", d.app.name)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	out, err := d.sdk.run(ctx, d.dev.serial, "forward", "tcp:0", "localabstract:"+socket)
	if err != nil {
		return fmt.Errorf("cannot reach axx's AR agent in the %s app: %w", d.app.name, err)
	}
	port, err := forwardedPort(out)
	if err != nil {
		return err
	}
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return fmt.Errorf("cannot reach axx's AR agent in the %s app: %w", d.app.name, err)
	}
	ctl := &arControl{conn: conn, r: bufio.NewReader(conn)}
	sdk, serial := d.sdk, d.dev.serial
	ctl.unforward = func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = sdk.run(ctx, serial, "forward", "--remove", "tcp:"+strconv.Itoa(port))
	}
	cam := d.app.camera
	cam.mu.Lock()
	old := cam.ar
	cam.ar = ctl
	cam.mu.Unlock()
	if old != nil {
		old.close()
	}
	d.scenario.OnClose(Name+".ar", func() error {
		ctl.close()
		return nil
	})
	return nil
}

// arControl is the bridge's socket, as adb forwards it.
type arControl struct {
	mu        sync.Mutex
	conn      net.Conn
	r         *bufio.Reader
	unforward func()
	closed    bool
}

// ask sends the bridge a line, and reads its answer's.
func (c *arControl) ask(ctx context.Context, line string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return "", errors.New("the app's AR has ended")
	}
	deadline := time.Now().Add(10 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = c.conn.SetDeadline(deadline)
	if _, err := io.WriteString(c.conn, line+"\n"); err != nil {
		return "", err
	}
	answer, err := c.r.ReadString('\n')
	return strings.TrimSpace(answer), err
}

func (c *arControl) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	_ = c.conn.Close()
	if c.unforward != nil {
		c.unforward()
	}
}

// poseLine is the bridge's line of a pose: its 16 numbers, column by column.
func poseLine(m scene.Mat4) string {
	parts := make([]string, len(m))
	for i, v := range m {
		parts[i] = strconv.FormatFloat(float64(v), 'g', -1, 32)
	}
	return strings.Join(parts, " ")
}

// SceneObject asks the bridge, in the app, where the scene's object shows on the screen, in
// pixels: once the app's AR camera view shows, and has the object in view (10 seconds).
func (d *running) SceneObject(ctx context.Context, name string) (float64, float64, error) {
	cam := d.app.camera
	if cam == nil {
		return 0, 0, fmt.Errorf("the %s app's camera sees no scene: give it one with \"the %s app's camera sees <scene>\"", d.app.name, d.app.name)
	}
	if cam.scene == nil {
		return 0, 0, core.Failf("The %s app's camera sees %s, which has no objects to tap: a scene names them", d.app.name, cam.sees)
	}
	if _, ok := cam.scene.Object(name); !ok {
		return 0, 0, core.Failf("The scene the %s app's camera sees has no %q: its objects are %s", d.app.name, name, cam.scene.ObjectNames())
	}
	deadline := time.Now().Add(10 * time.Second)
	reason := "the app has not started its AR"
	for {
		cam.mu.Lock()
		ctl := cam.ar
		cam.mu.Unlock()
		if ctl == nil && d.dev != nil && d.scenario != nil {
			if ok, _ := hasARCore(d.scenario, d.app.apk); !ok {
				return 0, 0, core.Failf("The %s app has no ARCore: a scene's objects show where its AR places them, and it has none", d.app.name)
			}
		}
		if ctl != nil {
			answer, err := ctl.ask(ctx, "where "+name)
			switch {
			case err != nil:
				reason = err.Error()
			case strings.HasPrefix(answer, "at "):
				f := strings.Fields(answer)
				x, errX := strconv.ParseFloat(f[1], 64)
				y, errY := strconv.ParseFloat(f[2], 64)
				if errX == nil && errY == nil {
					return x, y, nil
				}
				reason = answer
			default:
				reason = strings.TrimPrefix(strings.TrimPrefix(answer, "none "), "error ")
			}
		}
		if time.Now().After(deadline) {
			return 0, 0, fmt.Errorf("the %s app's camera view: %s", d.app.name, reason)
		}
		select {
		case <-ctx.Done():
			return 0, 0, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
