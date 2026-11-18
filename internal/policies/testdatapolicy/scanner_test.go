// scanner_test.go - Tests for candidate detection boundaries, context
// rule key-value extraction, Git index parsing, inventory
// classification (text, binary, symlink, submodule, unreadable),
// fail-closed enumeration, deterministic ordering and duplicate
// suppression.
//
// Forbidden sample values are assembled at runtime (see the helpers in
// policy_test.go) because this package scans its own sources.

package testdatapolicy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// memSource is an in-memory [Source].
type memSource struct {
	entries []IndexEntry
	files   map[string][]byte
	listErr error
	readErr map[string]error
}

func (m *memSource) Entries(context.Context) ([]IndexEntry, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.entries, nil
}

func (m *memSource) ReadFile(p string) ([]byte, error) {
	if err := m.readErr[p]; err != nil {
		return nil, err
	}
	data, ok := m.files[p]
	if !ok {
		return nil, fmt.Errorf("read %s: no such file", p)
	}
	return data, nil
}

// newMem builds a source of regular files from path/content pairs.
func newMem(pairs ...string) *memSource {
	m := &memSource{files: map[string][]byte{}}
	for i := 0; i+1 < len(pairs); i += 2 {
		m.entries = append(m.entries, IndexEntry{Mode: modeRegular, Path: pairs[i]})
		m.files[pairs[i]] = []byte(pairs[i+1])
	}
	return m
}

var testToday = time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)

func scanMem(t *testing.T, m *memSource) *Report {
	t.Helper()
	rep, err := Scan(t.Context(), m, Options{Today: testToday})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return rep
}

func TestScanAddressesBoundaries(t *testing.T) {
	priv := v4(10, 0, 0, 1)
	tests := []struct {
		name string
		line string
		want []string // raw values reported as candidates
	}{
		{"plain", "addr " + priv + " here", []string{priv}},
		{"host port", "dial " + priv + ":8080", []string{priv}},
		{"cidr", "net " + v4(10, 0, 0, 0) + "/24", []string{v4(10, 0, 0, 0) + "/24"}},
		{"end of sentence", "is " + priv + ".", []string{priv}},
		{"two on a line", priv + "-" + v4(10, 0, 0, 5), []string{priv, v4(10, 0, 0, 5)}},
		{"five part version", "v " + v4(20, 6, 5, 1) + ".1", nil},
		{"word prefix", "v" + priv, nil},
		{"section reference", "see section " + v4(6, 5, 1, 1), nil},
		{"version string", `"contentVersion": "` + v4(1, 0, 0, 0) + `"`, nil},
		{"escaped version string", `\"contentVersion\":\"` + v4(1, 0, 0, 0) + `\"`, nil},
		{"bracketed ipv6", "https://[2001:db8::1]:443/", []string{"2001:db8::1"}},
		{"ipv6 prefix", "p 2001:db8:100::/64,", []string{"2001:db8:100::/64"}},
		{"ipv6 sentence", "use 2001:db8::1.", []string{"2001:db8::1"}},
		{"identity separator", `prov + "::" + id`, []string{"::"}},
		{"cpp scope", "std::string", nil},
		{"aws type", "AWS::EC2::Instance", nil},
		{"time of day", "at 12:34:56 UTC", nil},
		{"mapped ipv4 once", v6("", "", "ffff", priv), []string{v6("", "", "ffff", priv)}},
		{"mac colon", "mac 00:00:5e:00:53:01 ok", []string{"00:00:5e:00:53:01"}},
		{"mac dotted", "mac 0000.5e00.5301", []string{"0000.5e00.5301"}},
		{"mac in long hex chain", "fp aa:bb:cc:dd:ee:ff:00:11:22", nil},
	}
	for _, tt := range tests {
		var got []string
		for _, c := range scanAddresses(tt.line, false) {
			got = append(got, c.raw)
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("%s: candidates %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestScanAddressesColumns(t *testing.T) {
	line := "x " + v4(10, 0, 0, 1)
	cs := scanAddresses(line, false)
	if len(cs) != 1 || cs[0].col != 3 {
		t.Fatalf("candidates %+v, want one at column 3", cs)
	}
}

func TestExtractKeyValues(t *testing.T) {
	tests := []struct {
		name, line string
		want       []string // key=value
	}{
		{"json", `{"hostName": "LAB-SW01", "port": 22}`, []string{"hostname=LAB-SW01", "port=22"}},
		{"compact json", `{"host-name":"LAB-SW01"}`, []string{"hostname=LAB-SW01"}},
		{"go struct", `{HostName: "LAB-SW01", Port: 22}`, []string{"hostname=LAB-SW01", "port=22"}},
		{"go short decl", `name := "LAB-SW01"`, []string{"name=LAB-SW01"}},
		{"yaml", `  hostname: LAB-SW01`, []string{"hostname=LAB-SW01"}},
		{"yaml single quote", `site: 'MXP'`, []string{"site=MXP"}},
		{"xml", `<host-name>LAB-SW01</host-name>`, []string{"hostname=LAB-SW01"}},
		{"xml attributes", `<host-name a="b">LAB-SW01</host-name>`, []string{"a=b", "hostname=LAB-SW01"}},
		{"xml mismatched", `<a>x</b>`, nil},
		{"xml go concatenation", `"<key>"+canary+"</key>"`, nil},
		{"raw string concatenation", "\"pass\": \"` + canary + `\",", nil},
		{"comparison", `if name == "x" {`, nil},
		{"escaped quote", `"name": "a\"b"`, []string{`name=a"b`}},
	}
	for _, tt := range tests {
		var got []string
		for _, kv := range extractKeyValues(tt.line) {
			got = append(got, kv.key+"="+kv.value)
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("%s: %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestScanContextScope(t *testing.T) {
	bad := `"hostname": "core` + `-sw01"`
	if fs, _ := scanContext("osiris/x/transform.go", bad); len(fs) != 0 {
		t.Errorf("production Go source got context findings %v", fs)
	}
	for _, p := range []string{"osiris/x/transform_test.go", "testdata/x.json", "README.md", "a.yaml"} {
		if fs, _ := scanContext(p, bad); len(fs) != 1 || fs[0].rule != RuleDevice {
			t.Errorf("%s: findings %v, want one %s", p, fs, RuleDevice)
		}
	}
	// Unquoted strings count only in configuration formats.
	unq := "hostname: core" + "-sw01"
	if fs, _ := scanContext("a.md", unq); len(fs) != 0 {
		t.Errorf("markdown unquoted value reported: %v", fs)
	}
	if fs, _ := scanContext("a.yml", unq); len(fs) != 1 {
		t.Errorf("yaml unquoted value not reported: %v", fs)
	}
}

func TestScanContextFreeText(t *testing.T) {
	at := "@"
	tests := []struct {
		name string
		path string
		line string
		rule string
	}{
		{"email ok", "a.md", "Jane <jane" + at + "example.com>", ""},
		{"email bad", "a.md", "Jane <jane" + at + "corp" + ".net>", RuleDNS},
		{"module version not email", "a.md", "go install x" + at + "v0.12.0", ""},
		{"url userinfo ok", "a_test.go", "postgres://admin:secret" + at + "db.example.com/x", ""},
		{"url userinfo bad", "a_test.go", "postgres://admin:s3" + "cret" + at + "db.example.com/x", RuleCredential},
		{"url then escaped newline", "a_test.go", `p := "https://LAB-API01.example.com\nnotify:ops` + at + `example.com"`, ""},
		{"asn label ok", "a.md", "peer AS64512 is fine", ""},
		{"asn label bad", "a.md", "peer " + "AS" + "1" + "3335 is real", RuleASN},
		{"cli host ok", "osiris/x/help.go", "  tool -h lab.example.com -u admin", ""},
		{"cli host bad", "osiris/x/help.go", "  tool -h switch" + ".lab -u admin", RuleDNS},
		{"cli host ip left to the address rules", "osiris/x/help.go", "  tool -h 192.0.2.1", ""},
		{"aws key shape", "osiris/x/x.go", "k := \"" + "AKIA" + strings.Repeat("Q", 16) + "\"", RuleCredential},
		{"pem shape", "osiris/x/x.go", "-----BEGIN RSA " + "PRIVATE KEY-----", RuleCredential},
		{"github token shape", "a.md", "ghp" + "_" + strings.Repeat("a", 36), RuleCredential},
	}
	for _, tt := range tests {
		fs, _ := scanContext(tt.path, tt.line)
		got := ""
		if len(fs) > 0 {
			got = fs[0].rule
		}
		if got != tt.rule {
			t.Errorf("%s: findings %v, want rule %q", tt.name, fs, tt.rule)
		}
	}
}

func TestParseIndex(t *testing.T) {
	rec := func(mode, p string) string { return mode + " 0123abcd 0\t" + p + "\x00" }
	out := rec(modeRegular, "b.go") + rec(modeExecutable, "a dir/with space.sh") +
		rec(modeRegular, "b.go") + rec(modeSymlink, "link")
	got, err := parseIndex([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	want := []IndexEntry{{modeExecutable, "a dir/with space.sh"}, {modeRegular, "b.go"}, {modeSymlink, "link"}}
	if !slices.Equal(got, want) {
		t.Errorf("parseIndex = %v, want %v", got, want)
	}

	for name, bad := range map[string]string{
		"empty":        "",
		"no tab":       "100644 abc 0 a.go\x00",
		"bad meta":     "100644 abc\ta.go\x00",
		"escaping":     rec(modeRegular, "../etc/passwd"),
		"absolute":     rec(modeRegular, "/etc/passwd"),
		"short mode":   "644 abc 0\ta.go\x00",
		"only NULs":    "\x00\x00",
		"dot segments": rec(modeRegular, "a/./b"),
	} {
		if _, err := parseIndex([]byte(bad)); err == nil {
			t.Errorf("%s: parseIndex accepted %q", name, bad)
		}
	}
}

func TestUntrackedRecords(t *testing.T) {
	got, err := parseIndex(untrackedRecords([]byte("new file.go\x00x/y.md\x00")))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Path != "new file.go" || got[0].Mode != modeRegular {
		t.Errorf("untracked entries = %v", got)
	}
}

func TestScanInventoryClassification(t *testing.T) {
	m := newMem("a.txt", "plain text\n", "bin.gif", "GIF89a\x00\x01", "latin1.txt", "caf\xe9\n")
	m.entries = append(m.entries,
		IndexEntry{Mode: modeSymlink, Path: "link"},
		IndexEntry{Mode: modeSubmodule, Path: "sub"},
		IndexEntry{Mode: "100664", Path: "odd"},
	)
	rep := scanMem(t, m)
	if rep.Files != 6 || rep.TextFiles != 1 || rep.OtherFiles != 5 {
		t.Errorf("counts files=%d text=%d other=%d, want 6 1 5", rep.Files, rep.TextFiles, rep.OtherFiles)
	}
	if len(rep.Violations) != 5 {
		t.Fatalf("violations %v, want 5 inventory findings", rep.Violations)
	}
	for _, f := range rep.Violations {
		if f.Rule != RuleInventory {
			t.Errorf("unexpected finding %v", f)
		}
		binary := f.Path == "bin.gif" || f.Path == "latin1.txt"
		if binary != (f.Fingerprint != "") {
			t.Errorf("%s: fingerprint %q; only binaries may be excepted", f.Path, f.Fingerprint)
		}
	}
	if !rep.Failed() {
		t.Error("report with inventory findings did not fail")
	}
}

func TestScanFailsClosed(t *testing.T) {
	m := newMem("a.txt", "x")
	m.listErr = errors.New("git: not a repository")
	if _, err := Scan(t.Context(), m, Options{Today: testToday}); err == nil {
		t.Error("enumeration failure did not fail the scan")
	}

	m = newMem("a.txt", "x", "b.txt", "y")
	m.readErr = map[string]error{"b.txt": errors.New("permission denied")}
	if _, err := Scan(t.Context(), m, Options{Today: testToday}); err == nil {
		t.Error("read failure did not fail the scan")
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Scan(ctx, newMem("a.txt", "x"), Options{Today: testToday}); err == nil {
		t.Error("cancelled context did not fail the scan")
	}
}

func TestGitSourceFailsOutsideRepository(t *testing.T) {
	if _, err := OpenRepository(t.Context(), t.TempDir()); err == nil {
		t.Error("OpenRepository succeeded outside a Git repository")
	}
}

func TestScanLineEndingsAndUTF8(t *testing.T) {
	priv := v4(10, 0, 0, 1)
	rep := scanMem(t, newMem("crlf.md", "caf\u00e9 ok\r\nhost "+priv+"\r\n"))
	if len(rep.Violations) != 1 || rep.Violations[0].Line != 2 || rep.Violations[0].Col != 6 {
		t.Fatalf("violations %v, want one at 2:6", rep.Violations)
	}
	if strings.Contains(rep.Violations[0].String(), "\r") {
		t.Error("finding contains a carriage return")
	}
}

func TestScanOrderingAndDedupe(t *testing.T) {
	priv := v4(10, 0, 0, 1)
	m := newMem(
		"z.md", "a "+priv+"\n",
		"a.md", "b "+priv+" and "+priv+"\nc "+v4(10, 0, 0, 2)+"\n",
	)
	// Entries arrive unsorted from this source; Scan output must not
	// depend on that order.
	rep := scanMem(t, m)
	var got []string
	for _, f := range rep.Violations {
		got = append(got, fmt.Sprintf("%s:%d:%d", f.Path, f.Line, f.Col))
	}
	want := []string{"a.md:1:3", "a.md:2:3", "z.md:1:3"}
	if !slices.Equal(got, want) {
		t.Errorf("order %v, want %v (same value twice on one line reported once)", got, want)
	}
}

func TestDedupeCredentialShapeAndField(t *testing.T) {
	key := "AKIA" + strings.Repeat("Q", 16)
	line := fmt.Sprintf(`{%q: %q}`, "api"+"_key", key)
	rep := scanMem(t, newMem("x_test.go", line))
	if len(rep.Violations) != 1 || rep.Violations[0].Rule != RuleCredential {
		t.Fatalf("violations %v, want one credential finding", rep.Violations)
	}
	if strings.Contains(rep.Violations[0].String(), key) {
		t.Error("credential finding prints the value")
	}
}

func TestFindingDisplayIsBounded(t *testing.T) {
	long := strings.Repeat("x", 500)
	if got := display(long); len(got) > maxDisplay+8 {
		t.Errorf("display length %d exceeds bound", len(got))
	}
}

func TestFingerprintStability(t *testing.T) {
	priv := v4(10, 0, 0, 1)
	fp := func(content string) []string {
		rep := scanMem(t, newMem("a.md", content))
		var out []string
		for _, f := range rep.Violations {
			out = append(out, f.Fingerprint)
		}
		return out
	}
	base := fp("x " + priv + "\n")
	moved := fp("\n\nnew line\nx " + priv + "\n")
	edited := fp("y " + priv + "\n")
	twice := fp("x " + priv + "\nx " + priv + "\n")
	if len(base) != 1 || !slices.Equal(base, moved) {
		t.Errorf("moving a line changed the fingerprint: %v vs %v", base, moved)
	}
	if slices.Equal(base, edited) {
		t.Error("editing the line kept the fingerprint")
	}
	if len(twice) != 2 || twice[0] != base[0] || twice[0] == twice[1] {
		t.Errorf("identical lines must get distinct ordinals: %v", twice)
	}
	if !reFingerprint.MatchString(base[0]) {
		t.Errorf("fingerprint %q has the wrong format", base[0])
	}
}

func TestSummary(t *testing.T) {
	rep := scanMem(t, newMem("a.md", "ok\n"))
	s := rep.Summary()
	for _, want := range []string{"Files inventoried: 1", "Text files scanned: 1", "Violations: 0", "Result: PASS"} {
		if !strings.Contains(s, want) {
			t.Errorf("summary missing %q:\n%s", want, s)
		}
	}
	if rep.Failed() {
		t.Error("clean report failed")
	}
}

func TestSummaryAllowlistState(t *testing.T) {
	const list = `{"exceptions":[]}`
	untracked := newMem("a.md", "ok\n")
	untracked.files[AllowlistFile] = []byte(list)
	tests := []struct {
		name  string
		src   *memSource
		state AllowlistState
		want  []string
		note  bool
	}{
		{"absent", newMem("a.md", "ok\n"), AllowlistAbsent, []string{"Allowlist: none"}, false},
		{"loaded", newMem("a.md", "ok\n", AllowlistFile, list), AllowlistLoaded, []string{"Allowlist: " + AllowlistFile + ", 0 entries applied"}, false},
		{"invalid", newMem(AllowlistFile, "{"), AllowlistInvalid, []string{"invalid, no entries applied", "Result: FAIL"}, false},
		{"untracked", untracked, AllowlistUnused, []string{"NOT USED (present but not tracked by Git)", "TDP_INCLUDE_UNTRACKED=1"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rep := scanMem(t, tt.src)
			if rep.Allowlist != tt.state {
				t.Errorf("state = %d, want %d", rep.Allowlist, tt.state)
			}
			s := rep.Summary()
			for _, w := range tt.want {
				if !strings.Contains(s, w) {
					t.Errorf("summary missing %q:\n%s", w, s)
				}
			}
			if got := strings.Contains(s, "Note:"); got != tt.note {
				t.Errorf("note present = %v, want %v:\n%s", got, tt.note, s)
			}
			if strings.Contains(s, "Inventory:") {
				t.Errorf("in-memory source must not report an inventory mode:\n%s", s)
			}
		})
	}
}

func TestGitSourceInventoryMode(t *testing.T) {
	g := &GitSource{}
	if got := g.InventoryMode(); got != "tracked files (Git index)" {
		t.Errorf("index mode = %q", got)
	}
	g.IncludeUntracked = true
	if got := g.InventoryMode(); !strings.Contains(got, "untracked") {
		t.Errorf("untracked mode = %q", got)
	}
}

func TestFindingString(t *testing.T) {
	f := Finding{Path: "a.md", Line: 2, Col: 3, Rule: RuleIPv4, Message: "m", Fingerprint: "sha256:00"}
	want := "a.md:2:3: OSIRIS-PR-TDP-001: m; use " + remedies[RuleIPv4] + " [sha256:00]"
	if got := f.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	f = Finding{Path: AllowlistFile, Rule: RuleException, Message: "m"}
	if got := f.String(); !strings.HasPrefix(got, AllowlistFile+": "+RuleException) {
		t.Errorf("String() = %q", got)
	}
}

// TestGitSourceRealRepository builds a throwaway repository with a
// regular file, a symlink pointing outside it and an untracked file,
// and checks the real Git-backed inventory end to end.
func TestGitSourceRealRepository(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "a file.md"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	git("add", "a file.md", "link")
	if err := os.WriteFile(filepath.Join(dir, "new.md"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	src, err := OpenRepository(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	entries, err := src.Entries(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := []IndexEntry{{modeRegular, "a file.md"}, {modeSymlink, "link"}}
	if !slices.Equal(entries, want) {
		t.Errorf("index entries %v, want %v", entries, want)
	}
	if _, err := src.ReadFile("link"); err == nil {
		t.Error("ReadFile followed a symlink")
	}
	if _, err := src.ReadFile("../outside.txt"); err == nil {
		t.Error("ReadFile escaped the repository root")
	}

	rep, err := Scan(t.Context(), src, Options{Today: testToday})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Violations) != 1 || rep.Violations[0].Path != "link" || rep.Violations[0].Rule != RuleInventory {
		t.Errorf("violations %v, want one symlink finding", rep.Violations)
	}

	src.IncludeUntracked = true
	entries, err = src.Entries(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || entries[2].Path != "new.md" {
		t.Errorf("entries with untracked %v, want new.md included", entries)
	}
}
