package mobileandroid

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protowire"
)

// emulatorControl is an emulator's own control service (gRPC), where axx sets its GPS, with the
// speed and bearing a console geo fix lacks, and what its camera sees, and where the phone is in
// it. axx starts its emulators with the service on a port of 127.0.0.1, and a token the emulator
// writes in its discovery file.
type emulatorControl struct {
	conn  *grpc.ClientConn
	token string
}

// dialEmulator connects to the control service of the emulator on port, once the emulator
// has written its token (it does as it starts).
func dialEmulator(ctx context.Context, port int) (*emulatorControl, error) {
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
	return &emulatorControl{conn: conn, token: token}, nil
}

// knot is a knot in metres a second. The emulator takes GpsState's speed in knots, though its
// proto says metres a second (emulator 37): it reports 7.1 m/s for 13.9.
const knot = 0.514444

// setGPS has the emulator report the phone at a point, moving at a speed (metres a second) toward
// a bearing (degrees clockwise from north).
func (g *emulatorControl) setGPS(ctx context.Context, lat, lon, speed, bearing float64) error {
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

// setEnvironment has the emulator's camera see a scene: an environment.ini's keys (scene.mode),
// as android.emulation.control.Environment carries them. The emulator writes them to its AVD's
// environment.ini and loads it, which it can only when the AVD had one as the emulator started.
func (g *emulatorControl) setEnvironment(ctx context.Context, keys map[string]string) error {
	var msg []byte
	for _, k := range slices.Sorted(maps.Keys(keys)) {
		var entry []byte
		entry = protowire.AppendTag(entry, 1, protowire.BytesType)
		entry = protowire.AppendString(entry, k)
		entry = protowire.AppendTag(entry, 2, protowire.BytesType)
		entry = protowire.AppendString(entry, keys[k])
		msg = protowire.AppendTag(msg, 1, protowire.BytesType)
		msg = protowire.AppendBytes(msg, entry)
	}
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+g.token)
	var out []byte
	return g.conn.Invoke(ctx, "/android.emulation.control.EmulatorController/setEnvironment", &msg, &out)
}

// setPhysical sets one of the phone's physical quantities in the emulator's model of it: where
// the phone is (position, in metres) or how it is turned (rotation, Euler angles in degrees),
// at once (step) or gliding there as a hand moves a phone, which its motion sensors then feel.
func (g *emulatorControl) setPhysical(ctx context.Context, target physical, values [3]float64, step bool) error {
	// android.emulation.control.PhysicalModelValue: target (1), value (3: ParameterValue, its
	// data (1) packed floats), interpolation (4: SMOOTH 0, STEP 1).
	var data, value, msg []byte
	for _, v := range values {
		data = protowire.AppendFixed32(data, math.Float32bits(float32(v)))
	}
	value = protowire.AppendTag(value, 1, protowire.BytesType)
	value = protowire.AppendBytes(value, data)
	msg = protowire.AppendTag(msg, 1, protowire.VarintType)
	msg = protowire.AppendVarint(msg, uint64(target))
	msg = protowire.AppendTag(msg, 3, protowire.BytesType)
	msg = protowire.AppendBytes(msg, value)
	if step {
		msg = protowire.AppendTag(msg, 4, protowire.VarintType)
		msg = protowire.AppendVarint(msg, 1)
	}
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+g.token)
	var out []byte
	return g.conn.Invoke(ctx, "/android.emulation.control.EmulatorController/setPhysicalModel", &msg, &out)
}

// physical is a PhysicalModelValue.PhysicalType.
type physical int

const (
	position physical = 0
	rotation physical = 1
)

func (g *emulatorControl) close() { _ = g.conn.Close() }

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
