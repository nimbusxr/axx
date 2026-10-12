package mobileandroid

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// emulatorGPS sets an emulator's GPS through the emulator's own control service (gRPC), with the
// speed and bearing a console geo fix lacks. The emulator then reports that state to Android
// every second, as a phone's GPS does. axx starts its emulators with the service on a port of
// 127.0.0.1, and a token the emulator writes in its discovery file.
type emulatorGPS struct {
	conn  *grpc.ClientConn
	token string
}

// dialEmulatorGPS connects to the control service of the emulator on port, once the emulator
// has written its token (it does as it starts).
func dialEmulatorGPS(ctx context.Context, port int) (*emulatorGPS, error) {
	var token string
	for {
		if token = emulatorToken(port); token != "" {
			break
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("the emulator's control service on port %d wrote no token in %s", port, strings.Join(discoveryDirs(), " or "))
		case <-time.After(500 * time.Millisecond):
		}
	}
	conn, err := grpc.NewClient("127.0.0.1:"+strconv.Itoa(port), grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(rawCodec{})))
	if err != nil {
		return nil, err
	}
	return &emulatorGPS{conn: conn, token: token}, nil
}

// knot is a knot in metres a second. The emulator takes GpsState's speed in knots, though its
// proto says metres a second (emulator 37): it reports 7.1 m/s for 13.9.
const knot = 0.514444

// set has the emulator report the phone at a point, moving at a speed (metres a second) toward
// a bearing (degrees clockwise from north).
func (g *emulatorGPS) set(ctx context.Context, lat, lon, speed, bearing float64) error {
	speed /= knot
	// android.emulation.control.GpsState (emulator_controller.proto in the SDK's emulator/lib):
	// passiveUpdate (1), latitude (2), longitude (3), speed (4), bearing (5), altitude (6),
	// satellites (7).
	msg := []byte{1 << 3, 1}
	for i, v := range []float64{lat, lon, speed, bearing} {
		msg = append(msg, byte((i+2)<<3|1))
		msg = binary.LittleEndian.AppendUint64(msg, math.Float64bits(v))
	}
	msg = append(msg, 7<<3, 8)
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+g.token)
	var out []byte
	return g.conn.Invoke(ctx, "/android.emulation.control.EmulatorController/setGps", &msg, &out)
}

func (g *emulatorGPS) close() { _ = g.conn.Close() }

// rawCodec passes protobuf messages encoded by hand.
type rawCodec struct{}

func (rawCodec) Marshal(v any) ([]byte, error) {
	b, ok := v.(*[]byte)
	if !ok {
		return nil, errors.New("not raw bytes")
	}
	return *b, nil
}

func (rawCodec) Unmarshal(data []byte, v any) error {
	b, ok := v.(*[]byte)
	if !ok {
		return errors.New("not raw bytes")
	}
	*b = append((*b)[:0], data...)
	return nil
}

func (rawCodec) Name() string { return "raw" }

// emulatorToken is the token of the emulator whose control service is on port, from its
// discovery file; "" until the emulator writes it.
func emulatorToken(port int) string {
	want := "grpc.port=" + strconv.Itoa(port)
	for _, dir := range discoveryDirs() {
		files, _ := filepath.Glob(filepath.Join(dir, "pid_*.ini"))
		for _, f := range files {
			if token := tokenIn(f, want); token != "" {
				return token
			}
		}
	}
	return ""
}

func tokenIn(path, port string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	var token string
	ours := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == port {
			ours = true
		} else if v, ok := strings.CutPrefix(line, "grpc.token="); ok {
			token = v
		}
	}
	if !ours {
		return ""
	}
	return token
}

// discoveryDirs are where emulators write their discovery files (what Android Studio reads),
// by platform: the emulator's -help-grpc-use-token names this platform's.
func discoveryDirs() []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return []string{filepath.Join(home, "Library", "Caches", "TemporaryItems", "avd", "running")}
	case "windows":
		return []string{filepath.Join(os.Getenv("LOCALAPPDATA"), "Temp", "avd", "running")}
	}
	dirs := []string{}
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		dirs = append(dirs, filepath.Join(xdg, "avd", "running"))
	}
	user := os.Getenv("USER")
	return append(dirs, filepath.Join(os.TempDir(), "android-"+user, "avd", "running"), filepath.Join(home, ".android", "avd", "running"))
}
