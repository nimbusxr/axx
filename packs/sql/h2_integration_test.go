//go:build integration

package sql

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	dbsql "database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	mobycontainer "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/nimbusxr/axx/internal/mirror"
	"github.com/nimbusxr/axx/packs/sql/internal/h2"
)

// h2Releases are the H2 releases the client is held to, with their jars' SHA-256
// sums: the last of H2 1.4, whose protocol and types are the old ones, the last of
// H2 2.1, which many services still have, and the latest.
var h2Releases = []struct{ version, sum string }{
	{"1.4.200", "3ad9ac4b6aae9cd9d3ac1c447465e1ed06019b851b893dd6a8d76ddb6d85bca6"},
	{"2.1.214", "d623cdc0f61d218cf549a8d09f1c391ff91096116b22e2475475fce4fbe72bd0"},
	{"2.5.252", "90b11dc413da82070ef15fc943282c1408e6ffd34fa68998335fea8603b5ff20"},
}

const h2Image = "eclipse-temurin:21-jre-alpine"

// h2Jar is H2's jar of a release, from Maven Central (or AXX_MAVEN_URL), kept in
// the machine's cache.
func h2Jar(t *testing.T, version, sum string) string {
	t.Helper()
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cache, "axx-test", "h2", "h2-"+version+".jar")
	sums := func(b []byte) bool {
		s := sha256.Sum256(b)
		return hex.EncodeToString(s[:]) == sum
	}
	if b, err := os.ReadFile(path); err == nil && sums(b) {
		return path
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mirror.Maven("/com/h2database/h2/"+version+"/h2-"+version+".jar"), nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Skipf("H2 %s unavailable: %v", version, err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil || res.StatusCode != http.StatusOK {
		t.Skipf("H2 %s unavailable: %s %v", version, res.Status, err)
	}
	if !sums(b) {
		t.Fatalf("H2 %s's jar is not the one this test pins", version)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// h2Container runs H2's jar in a container that has Java, in the databases' zone.
func h2Container(t *testing.T, jar string, port, ready string, cmd []string, modify func(*testcontainers.ContainerRequest)) (host, mapped string, c testcontainers.Container) {
	t.Helper()
	ctx := context.Background()
	req := testcontainers.ContainerRequest{
		Image: h2Image, ExposedPorts: []string{port + "/tcp"}, Env: map[string]string{"TZ": databaseZone},
		Cmd:   cmd,
		Files: []testcontainers.ContainerFile{{HostFilePath: jar, ContainerFilePath: "/h2.jar", FileMode: 0o644}},
		// Its line, and its port mapped: Docker says a container runs before it has
		// published its ports.
		WaitingFor: wait.ForAll(wait.ForLog(ready), wait.ForListeningPort(port+"/tcp")).WithDeadline(2 * time.Minute),
	}
	if modify != nil {
		modify(&req)
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: req, Started: true})
	if c != nil {
		t.Cleanup(func() { _ = c.Terminate(ctx) })
	}
	if err != nil && c != nil && c.IsRunning() {
		t.Fatalf("H2 did not get ready: %v", err) // not a machine without Docker: a test that would prove nothing
	}
	if err != nil {
		t.Skipf("%s unavailable: %v", h2Image, err)
	}
	host, err = c.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.MappedPort(ctx, port+"/tcp")
	if err != nil {
		t.Fatal(err)
	}
	return host, p.Port(), c
}

// h2Server starts a real H2 server of a release, with a database in its memory,
// and with TLS when scheme is ssl.
func h2Server(t *testing.T, version, sum, scheme string) (url string) {
	t.Helper()
	cmd := []string{"java", "-Duser.timezone=" + databaseZone, "-cp", "/h2.jar", "org.h2.tools.Server", "-tcp", "-tcpAllowOthers", "-ifNotExists"}
	if scheme == "ssl" {
		cmd = append(cmd, "-tcpSSL")
	}
	host, port, _ := h2Container(t, h2Jar(t, version, sum), "9092", "TCP server running", cmd, nil)
	return fmt.Sprintf("jdbc:h2:%s://%s:%s/mem:parcels;DB_CLOSE_DELAY=-1", scheme, host, port)
}

func h2ForTest(t *testing.T, url string) *dbsql.DB {
	t.Helper()
	cfg, err := h2.ParseURL(url, "sa", "")
	if err != nil {
		t.Fatal(err)
	}
	db := dbsql.OpenDB(h2.NewConnector(cfg))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestH2(t *testing.T) {
	for _, release := range h2Releases {
		t.Run(release.version, func(t *testing.T) {
			t.Parallel()
			two := !strings.HasPrefix(release.version, "1.")
			url := h2Server(t, release.version, release.sum, "tcp")
			db := h2ForTest(t, url)
			always := "id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY"
			if !two {
				always = "id BIGINT AUTO_INCREMENT PRIMARY KEY"
			}
			for _, s := range []string{
				`CREATE TABLE pickups (
					reference VARCHAR(40) PRIMARY KEY, postcode VARCHAR(10), code CHAR(5), note CLOB, grams INT, label BIGINT, tries SMALLINT,
					kilos DECIMAL(10,2), rate DOUBLE, ratio REAL, thin REAL, signed_for BOOLEAN, due DATE, opens TIME, planned_at TIMESTAMP,
					zoned_at TIMESTAMP WITH TIME ZONE, status ENUM('PLANNED','DONE'), details JSON, photo BLOB, tracking UUID)`,
				`INSERT INTO pickups VALUES ('PX-TYPES-1', '10115', 'H91E2', 'ring twice', 850, 3000000000, 2, 12.50, 1.5, 0.5, 0.1, TRUE,
					'2026-09-24', '07:40:00', '2026-09-24 07:40:00', '2026-09-24 07:40:00+02:00', 'PLANNED', '{"zone":"DE-1","count":3}' FORMAT JSON, X'78AB',
					'0f8fad5b-d9cb-469f-a165-70867728950e')`,
				`INSERT INTO pickups (reference, grams) VALUES ('ABC-OTHER', 1)`,
				`CREATE TABLE seeded (id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY, reference VARCHAR(40), kilos DECIMAL(10,2), flagged BOOLEAN, due DATE,
					planned_at TIMESTAMP, zoned_at TIMESTAMP WITH TIME ZONE, created_at TIMESTAMP, details JSON, photo BLOB)`,
				`CREATE TABLE always_id (` + always + `, reference VARCHAR(40))`,
				`CREATE SCHEMA depot`,
				`CREATE TABLE depot.shelves (code VARCHAR(10) PRIMARY KEY, parcels INT)`,
			} {
				if _, err := db.Exec(s); err != nil {
					t.Fatalf("%s: %v", s, err)
				}
			}
			h := newHarness(t)
			h.ok("a parcels-db database with the following properties:", []string{"url", url}, []string{"user", "sa"}, []string{"password", ""})

			t.Run("values are held to their columns", func(t *testing.T) {
				columnCases{
					table: "pickups",
					fails: [][3]string{
						{"reference", "0", `reference is text in pickups (CHARACTER VARYING): write "0"`},
						{"postcode", "10115", `postcode is text in pickups (CHARACTER VARYING): write "10115"`},
						{"code", "12345", `code is text in pickups (CHARACTER): write "12345"`},
						{"note", "true", `note is text in pickups (CHARACTER LARGE OBJECT): write "true"`},
						{"grams", `"850"`, `grams is an integer in pickups (INTEGER): write 850`},
						{"grams", "850.0", `grams is an integer in pickups (INTEGER): write 850`},
						{"label", "3000000000.0", `label is an integer in pickups (BIGINT): write 3000000000`},
						{"tries", "true", `tries is an integer in pickups (SMALLINT)`},
						{"kilos", "12", `kilos is a decimal in pickups (NUMERIC): write 12.0`},
						{"rate", "2", `rate is a decimal in pickups (DOUBLE PRECISION)`},
						{"ratio", "1", `ratio is a decimal in pickups (REAL)`},
						{"signed_for", "1", `signed_for is a boolean in pickups (BOOLEAN)`},
						{"signed_for", `"true"`, `signed_for is a boolean in pickups (BOOLEAN)`},
						{"due", "2026", `due is written as a string in pickups (DATE): write "2026"`},
						{"planned_at", `"2026-09-24T07:40:00+02:00"`, `planned_at has no time zone in pickups (TIMESTAMP)`},
						{"zoned_at", `"2026-09-24 07:40:00"`, `zoned_at holds a moment in pickups (TIMESTAMP WITH TIME ZONE)`},
					},
					finds: [][2]string{
						{"reference", "PX-TYPES-1"},
						{"postcode", `"10115"`},
						{"code", "H91E2"},
						{"note", "ring twice"},
						{"grams", "850"},
						{"label", "3000000000"},
						{"tries", "2"},
						{"kilos", "12.5"},
						{"kilos", "12.50"},
						{"rate", "1.5"},
						{"ratio", "0.5"},
						{"thin", "0.1"},
						{"signed_for", "true"},
						{"due", `"2026-09-24"`},
						{"opens", `"07:40:00"`},
						{"planned_at", `"2026-09-24 07:40:00"`},
						{"planned_at", `"2026-09-24T07:40:00"`},
						{"zoned_at", `"2026-09-24T05:40:00Z"`},
						{"zoned_at", `"2026-09-24 07:40:00+02:00"`},
						{"status", "PLANNED"},
						{"details", `{"zone":"DE-1","count":3}`},
						{"details", `{"zone": "DE-1", "count": 3}`},
						{"tracking", "0f8fad5b-d9cb-469f-a165-70867728950e"},
						{"photo", "78AB"},
						{"photo", "78ab"},
					},
					// H2 compares JSON as its text, whatever its spaces: its names in their order.
					misses: [][2]string{
						{"reference", "PX-NONE"},
						{"grams", "1000"},
						{"kilos", "3.0"},
						{"postcode", `"99999"`},
						{"signed_for", "false"},
						{"status", "DONE"},
						{"details", `{"count":3,"zone":"DE-1"}`},
					},
				}.run(t, h, true)
			})

			t.Run("a column's value", func(t *testing.T) {
				cellCases{
					table: "pickups",
					passes: [][2]string{
						{"reference", "PX-TYPES-1"},
						{"postcode", `"10115"`},
						{"code", "H91E2"},
						{"note", "ring twice"},
						{"grams", "850"},
						{"label", "3000000000"},
						{"tries", "2"},
						{"kilos", "12.5"},
						{"kilos", "12.50"},
						{"rate", "1.5"},
						{"ratio", "0.5"},
						{"thin", "0.1"},
						{"signed_for", "true"},
						{"due", "2026-09-24"},
						{"opens", "07:40:00"},
						{"planned_at", "2026-09-24 07:40:00"},
						{"zoned_at", "2026-09-24T05:40:00Z"},
						{"status", "PLANNED"},
						{"details", `{"zone":"DE-1","count":3}`},
						{"tracking", "0f8fad5b-d9cb-469f-a165-70867728950e"},
						{"photo", "78AB"},
					},
					fails: [][3]string{
						{"postcode", "10115", `"10115"`},
						{"grams", `"850"`, "850"},
						{"kilos", "12", "differs"},
						{"due", "2026-09-25", "differs"},
						{"signed_for", "1", "differs"},
						{"zoned_at", "2026-09-24 07:40:00", "holds a moment"},
					},
				}.run(t, h)
			})

			t.Run("a column's JSON", func(t *testing.T) {
				h.ok("a selection of rows is retrieved from the pickups table where:", []string{"reference", "PX-TYPES-1"})
				n := nth(len(mustService(t, h).Selections()))
				h.ok(fmt.Sprintf("the 1st row details property for the %s selection json properties are:", n),
					[]string{"zone", "DE-1"}, []string{"count", "3"})
				if err := h.step(fmt.Sprintf("the 1st row details property for the %s selection json properties are:", n), []string{"count", `"3"`}); err == nil {
					t.Error(`"3" matched the number 3`)
				}
			})

			t.Run("seeds", func(t *testing.T) {
				h.file("seed.yaml", `seeded:
  - id: 7001
    reference: PX-H2-1
    kilos: 2.5
    flagged: true
    due: "2026-09-24"
    planned_at: "2026-09-24 07:40:00.123"
    zoned_at: "2026-09-24T07:40:00+02:00"
    created_at: "[DAY,PLUS,1]"
    details: {zone: DE-1, stops: [1, 2]}
    photo: CAFE
  - reference: PX-H2-2
    kilos: "[null]"
    flagged: false
    zoned_at: "[HOUR,MINUS,2]"
    planned_at: "2026-09-24T07:40:00"
always_id:
  - id: 41
    reference: PX-H2-ALWAYS
depot.shelves:
  - code: A-1
    parcels: 3
`)
				h.ok("a seed.yaml db seed")
				h.ok("a selection of rows is retrieved from the seeded table where:",
					[]string{"id", "7001"}, []string{"reference", "PX-H2-1"}, []string{"kilos", "2.5"}, []string{"flagged", "true"}, []string{"due", `"2026-09-24"`},
					[]string{"zoned_at", `"2026-09-24T05:40:00Z"`}, []string{"details", `{"stops":[1,2],"zone":"DE-1"}`}, []string{"photo", "CAFE"})
				svc := mustService(t, h)
				last := func() *Selection {
					all := svc.Selections()
					return all[len(all)-1]
				}
				if len(last().Rows) != 1 {
					t.Fatalf("seeded row: %v", last().Rows)
				}
				// A day from now by the session's clock, and two hours ago to the minute.
				var days, minutes int
				if err := db.QueryRow("SELECT DATEDIFF('DAY', CURRENT_DATE, created_at) FROM seeded WHERE id = 7001").Scan(&days); err != nil {
					t.Fatal(err)
				}
				if days != 1 {
					t.Errorf("created_at is %d days from the database's clock, want 1", days)
				}
				if err := db.QueryRow("SELECT DATEDIFF('MINUTE', zoned_at, CURRENT_TIMESTAMP) FROM seeded WHERE reference = 'PX-H2-2'").Scan(&minutes); err != nil {
					t.Fatal(err)
				}
				if minutes < 119 || minutes > 121 {
					t.Errorf("zoned_at is %d minutes ago, want 120", minutes)
				}
				h.ok("a selection of rows is retrieved from the seeded table where:",
					[]string{"reference", "PX-H2-2"}, []string{"kilos", "null"}, []string{"flagged", "false"})
				if len(last().Rows) != 1 {
					t.Errorf("a row with a null and a false: %v", last().Rows)
				}
				h.ok("a selection of rows is retrieved from the always_id table where:", []string{"id", "41"}, []string{"reference", "PX-H2-ALWAYS"})
				if len(last().Rows) != 1 {
					t.Errorf("a row that sets its identity column: %v", last().Rows)
				}
				h.ok("a selection of rows is retrieved from the depot.shelves table where:", []string{"code", "A-1"}, []string{"parcels", "3"})
				if len(last().Rows) != 1 {
					t.Errorf("a row of another schema's table: %v", last().Rows)
				}
			})

			t.Run("a seed's mistakes say what to write", func(t *testing.T) {
				for _, c := range []struct{ seed, want string }{
					{"seeded:\n  - id: 7001\n    reference: PX-H2-1\n", "a row with that key is already there"},
					{"seeded:\n  - no_such_column: PX-H2-3\n", "seeded has the columns ID, REFERENCE"},
					{"seedd:\n  - reference: PX-H2-3\n", "has the tables ALWAYS_ID, PICKUPS, SEEDED"},
					{"depot.shelfs:\n  - code: A-2\n", "the schema depot has the tables SHELVES"},
				} {
					h.file("bad.yaml", c.seed)
					if err := h.step("a bad.yaml db seed"); err == nil || !strings.Contains(err.Error(), c.want) {
						t.Errorf("%q: %v, want %s", c.seed, err, c.want)
					}
				}
			})

			t.Run("row locks", func(t *testing.T) {
				h.ok("the rows in the pickups table are locked where:", []string{"reference", "PX-TYPES-1"})
				if _, err := db.Exec("UPDATE pickups SET grams = 851 WHERE reference = 'PX-TYPES-1'"); err == nil {
					t.Error("the row is not locked")
				}
				h.ok("the row locks are released")
				if _, err := db.Exec("UPDATE pickups SET grams = 850 WHERE reference = 'PX-TYPES-1'"); err != nil {
					t.Errorf("after releasing: %v", err)
				}
			})

			t.Run("a row someone else holds is waited for, ten seconds", func(t *testing.T) {
				tx, err := db.Begin()
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = tx.Rollback() }()
				if _, err := tx.Exec("SELECT * FROM pickups WHERE reference = 'ABC-OTHER' FOR UPDATE"); err != nil {
					t.Fatal(err)
				}
				start := time.Now()
				err = h.step("the rows in the pickups table are locked where:", []string{"reference", "ABC-OTHER"})
				took := time.Since(start)
				if err == nil || !strings.Contains(err.Error(), "Could not acquire row lock") || took < 9*time.Second || took > 20*time.Second {
					t.Errorf("after %s: %v", took, err)
				}
				// Released, the row is the step's to lock.
				if err := tx.Rollback(); err != nil {
					t.Fatal(err)
				}
				h.ok("the row locks are released")
				h.ok("the rows in the pickups table are locked where:", []string{"reference", "ABC-OTHER"})
				h.ok("the row locks are released")
			})

			t.Run("a statement that runs past its time is stopped", func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
				defer cancel()
				start := time.Now()
				// Minutes of counting, were it left to.
				_, err := db.ExecContext(ctx, "SELECT COUNT(*) FROM SYSTEM_RANGE(1, 100000) a, SYSTEM_RANGE(1, 100000) b")
				took := time.Since(start)
				if !errors.Is(err, context.DeadlineExceeded) || took < 400*time.Millisecond || took > 10*time.Second {
					t.Errorf("after %s: %v", took, err)
				}
				// The server stopped it, and the session is as it was.
				var one int
				if err := db.QueryRow("SELECT 1").Scan(&one); err != nil {
					t.Errorf("after a stopped statement: %v", err)
				}
				if stats := db.Stats(); stats.OpenConnections != 1 {
					t.Errorf("%d connections open, want the one the statement ran on", stats.OpenConnections)
				}
			})

			t.Run("the session's time zone", func(t *testing.T) {
				offset := func(url string) int {
					t.Helper()
					var now time.Time
					if err := h2ForTest(t, url).QueryRow("SELECT CAST(CURRENT_TIMESTAMP AS TIMESTAMP WITH TIME ZONE)").Scan(&now); err != nil {
						t.Fatal(err)
					}
					if d := time.Since(now); d < -time.Minute || d > time.Minute {
						t.Errorf("the database's clock reads %s", now)
					}
					_, seconds := now.Zone()
					return seconds
				}
				_, here := time.Now().Zone()
				if !two {
					// H2 1.4 has one zone, its server's, whatever a URL says.
					if got := offset(url + ";TIME ZONE=Asia/Tokyo"); got != 5*3600+1800 {
						t.Errorf("H2 1.4's sessions are %d seconds from UTC, want %s's", got, databaseZone)
					}
					return
				}
				if got := offset(url); got != here {
					t.Errorf("a session is %d seconds from UTC, want this machine's %d", got, here)
				}
				if got := offset(url + ";TIME ZONE=Asia/Tokyo"); got != 9*3600 {
					t.Errorf("a session of a URL that names Asia/Tokyo is %d seconds from UTC", got)
				}
			})

			t.Run("a user and its password", func(t *testing.T) {
				if _, err := db.Exec("CREATE USER parcels PASSWORD 'Parcels-Secret-1 ü' ADMIN"); err != nil {
					t.Fatal(err)
				}
				h := newHarness(t)
				h.ok("a parcels-db database with the following properties:", []string{"url", url}, []string{"user", "parcels"}, []string{"password", "Parcels-Secret-1 ü"})
				h.ok("a selection of rows is retrieved from the pickups table where:", []string{"reference", "PX-TYPES-1"})
				wrong := newHarness(t)
				wrong.ok("a parcels-db database with the following properties:", []string{"url", url}, []string{"user", "parcels"}, []string{"password", "another"})
				err := wrong.step("a selection of rows is retrieved from the pickups table where:", []string{"reference", "PX-TYPES-1"})
				if err == nil || !strings.Contains(err.Error(), "Wrong user name or password") {
					t.Errorf("a wrong password: %v", err)
				}
			})

			t.Run("triggers are another database's", func(t *testing.T) {
				err := h.step("a before insert trigger on the pickups table will raise an 23505 exception 1 time where:", []string{"reference", "PX-H2-T1"})
				if err == nil || !strings.Contains(err.Error(), "PostgreSQL") {
					t.Errorf("a trigger on H2: %v", err)
				}
			})
		})
	}
}

// A server started with -tcpSSL is reached over TLS (jdbc:h2:ssl:), when its
// certificate is one the machine trusts. H2's built-in certificate, which a server
// shows unless it is given another, is not: H2's own client refuses it too.
func TestH2OverTLS(t *testing.T) {
	release := h2Releases[len(h2Releases)-1]

	t.Run("H2's built-in certificate is refused", func(t *testing.T) {
		url := h2Server(t, release.version, release.sum, "ssl")
		h := newHarness(t)
		h.ok("a parcels-db database with the following properties:", []string{"url", url}, []string{"user", "sa"}, []string{"password", ""})
		err := h.step("a selection of rows is retrieved from the parcels table where:", []string{"reference", "PX-TLS-1"})
		if err == nil || !strings.Contains(err.Error(), "is not one this machine trusts") {
			t.Errorf("a server with H2's built-in certificate: %v", err)
		}
	})

	t.Run("a certificate of the server's own is trusted by who trusts it", func(t *testing.T) {
		const store = "-keystore /tmp/h2.p12 -storetype PKCS12 -storepass parcels-store -alias h2"
		host, port, c := h2Container(t, h2Jar(t, release.version, release.sum), "9092", "TCP server running", []string{
			"sh", "-c",
			"keytool -genkeypair -keyalg RSA -keysize 2048 -validity 2 -dname CN=parcels-db -ext SAN=dns:localhost,ip:127.0.0.1 " + store +
				" && keytool -exportcert -rfc -file /tmp/h2.pem " + store +
				" && exec java -Djavax.net.ssl.keyStore=/tmp/h2.p12 -Djavax.net.ssl.keyStorePassword=parcels-store" +
				" -cp /h2.jar org.h2.tools.Server -tcp -tcpSSL -tcpAllowOthers -ifNotExists",
		}, nil)
		r, err := c.CopyFileFromContainer(context.Background(), "/tmp/h2.pem")
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		pem, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pem) {
			t.Fatalf("the server's certificate: %s", pem)
		}
		cfg, err := h2.ParseURL(fmt.Sprintf("jdbc:h2:ssl://%s:%s/mem:parcels;DB_CLOSE_DELAY=-1", host, port), "sa", "")
		if err != nil {
			t.Fatal(err)
		}
		cfg.RootCAs = roots
		db := dbsql.OpenDB(h2.NewConnector(cfg))
		defer db.Close()
		if _, err := db.Exec("CREATE TABLE parcels (reference VARCHAR(40) PRIMARY KEY, kilos DECIMAL(10,2))"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("INSERT INTO parcels VALUES ('PX-TLS-1', 2.50)"); err != nil {
			t.Fatal(err)
		}
		var kilos string
		if err := db.QueryRow("SELECT kilos FROM parcels WHERE reference = 'PX-TLS-1'").Scan(&kilos); err != nil || kilos != "2.50" {
			t.Errorf("over TLS: %q, %v", kilos, err)
		}
	})
}

// A service that opens a file database for itself with AUTO_SERVER=TRUE listens for
// others: axx reads where, and with what key, in the lock file beside the database.
func TestH2AutoServer(t *testing.T) {
	release := h2Releases[len(h2Releases)-1]
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	// The same port in the container and on this machine: the lock file names the
	// container's address, and where that is out of reach, the port is this machine's.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
	_ = l.Close()
	// The service: H2's own shell, which has the database open for as long as it runs.
	_, _, _ = h2Container(t, h2Jar(t, release.version, release.sum), port, "Welcome to H2 Shell",
		[]string{"java", "-cp", "/h2.jar", "org.h2.tools.Shell", "-url", "jdbc:h2:file:/data/parcels;AUTO_SERVER=TRUE;AUTO_SERVER_PORT=" + port, "-user", "sa", "-password", ""},
		func(req *testcontainers.ContainerRequest) {
			req.ConfigModifier = func(c *mobycontainer.Config) { c.OpenStdin, c.Tty = true, true }
			req.HostConfigModifier = func(hc *mobycontainer.HostConfig) {
				hc.Binds = []string{dir + ":/data"}
				hc.PortBindings = network.PortMap{network.MustParsePort(port + "/tcp"): {
					{HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: port},
				}}
			}
		})
	url := "jdbc:h2:file:" + filepath.ToSlash(filepath.Join(dir, "parcels")) + ";AUTO_SERVER=TRUE"
	if _, err := h2ForTest(t, url).Exec("CREATE TABLE parcels (reference VARCHAR(40) PRIMARY KEY, kilos DECIMAL(10,2))"); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t)
	h.ok("a parcels-db database with the following properties:", []string{"url", url}, []string{"user", "sa"}, []string{"password", ""})
	h.file("seed.yaml", "parcels:\n  - reference: PX-AUTO-1\n    kilos: 2.5\n")
	h.ok("a seed.yaml db seed")
	h.ok("a selection of rows is retrieved from the parcels table where:", []string{"reference", "PX-AUTO-1"}, []string{"kilos", "2.5"})
	if all := mustService(t, h).Selections(); len(all[len(all)-1].Rows) != 1 {
		t.Errorf("the seeded row: %v", all[len(all)-1].Rows)
	}

	t.Run("a database no service has open says so", func(t *testing.T) {
		h := newHarness(t)
		h.ok("a closed-db database with the following properties:",
			[]string{"url", "jdbc:h2:file:" + filepath.ToSlash(filepath.Join(dir, "closed")) + ";AUTO_SERVER=TRUE"}, []string{"user", "sa"}, []string{"password", ""})
		err := h.step("a selection of rows is retrieved from the parcels table where:", []string{"reference", "PX-AUTO-1"})
		if err == nil || !strings.Contains(err.Error(), "no service has the database open") {
			t.Errorf("a database that is not open: %v", err)
		}
	})
}
