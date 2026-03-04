// batch.go - CSV parsing and batch orchestration for Cisco producers.
// Provides CSV template generation, target parsing with datacenter hierarchy,
// and a RunBatch function that writes OSIRIS documents to a hierarchical
// directory structure (DC/Floor/Room/Zone/Hostname.json).
//
// For an introduction to OSIRIS JSON Producer for Cisco see:
// "[OSIRIS-JSON-CISCO]."
//
// [OSIRIS-JSON-CISCO]: https://osirisjson.org/en/docs/developers/producers/cisco/

package shared

import (
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"go.osirisjson.org/producers/pkg/sdk"
)

// ProducerFactory creates a Producer for a given target and run configuration.
// Each sub-producer (APIC, NX-OS, IOS-XR) registers its own factory that
// builds the appropriate transport (HTTP or SSH) internally.
type ProducerFactory func(target TargetConfig, cfg *RunConfig) sdk.Producer

// FactoryRegistry maps producer type names to their factory functions.
// Used by RunBatch to dispatch to the correct producer per CSV row.
type FactoryRegistry map[string]ProducerFactory

// CSVTemplate returns a CSV template string with header, column documentation,
// owner value descriptions, and precompiled example rows showing all three Cisco sub-producer types.
func CSVTemplate(producerName string) string {
	return fmt.Sprintf(`# OSIRIS CSV batch template for Cisco %s
# Lines starting with # are comments and will be ignored.
#
# Columns:
#   dc        - Datacenter name (used for output folder hierarchy)
#   floor     - Floor identifier within the datacenter
#   room      - Room identifier within the floor
#   zone      - Zone or pod identifier within the room
#   hostname  - Device label used as output filename (required)
#   type      - Producer type: apic, nxos, iosxr (required)
#   ip        - IP address or FQDN of the target device (required)
#   port      - Override port (optional; default: producer-specific)
#   owner     - Device ownership for operator reference (does not affect collection):
#                 self  - your own device (default if omitted)
#                 isp   - ISP-managed device you have read access to
#                 colo  - colocation provider equipment
#   notes     - Free-text operator notes (ignored by producer)
#
# Credentials are provided via --username/--password flags
# and apply to all targets in the batch.
#
# Output hierarchy: <output-dir>/DC/Floor/Room/Zone/Hostname.json
# Empty location fields are skipped (e.g. no floor → DC/Room/Zone/Hostname.json).
#
# Example:
dc,floor,room,zone,hostname,type,ip,port,owner,notes
AMS-01,F3,R301,POD-A,%[1]s-01,%[1]s,10.10.1.1,,self,Primary %[1]s controller
AMS-01,F3,R301,POD-A,nx-spine-01,nxos,10.10.1.10,,self,Spine switch rack A1
AMS-01,F3,R302,POD-B,isp-pe-router,iosxr,172.16.0.1,,isp,ISP PE router - read-only access
`, producerName)
}

// csvColumns defines the recognized column names and their indices.
type csvColumns struct {
	dc int
	floor int
	room int
	zone int
	hostname int
	typ int
	ip int
	port int
	owner int
	notes int
}

// resolveColumns maps header names to column indices.
// Returns an error if required columns (hostname, type, ip) are missing.
func resolveColumns(header []string) (*csvColumns, error) {
	idx := map[string]int{}
	for i, col := range header {
		idx[strings.TrimSpace(strings.ToLower(col))] = i
	}

	col := &csvColumns{
		dc: -1, floor: -1, room: -1, zone: -1,
		hostname: -1, typ: -1, ip: -1, port: -1,
		owner: -1, notes: -1,
	}

	// Required columns.
	var missing []string
	if v, ok := idx["hostname"]; ok {
		col.hostname = v
	} else {
		missing = append(missing, "hostname")
	}
	if v, ok := idx["type"]; ok {
		col.typ = v
	} else {
		missing = append(missing, "type")
	}
	if v, ok := idx["ip"]; ok {
		col.ip = v
	} else {
		missing = append(missing, "ip")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("CSV missing required columns: %s", strings.Join(missing, ", "))
	}

	// Optional columns.
	if v, ok := idx["dc"]; ok {
		col.dc = v
	}
	if v, ok := idx["floor"]; ok {
		col.floor = v
	}
	if v, ok := idx["room"]; ok {
		col.room = v
	}
	if v, ok := idx["zone"]; ok {
		col.zone = v
	}
	if v, ok := idx["port"]; ok {
		col.port = v
	}
	if v, ok := idx["owner"]; ok {
		col.owner = v
	}
	if v, ok := idx["notes"]; ok {
		col.notes = v
	}

	return col, nil
}

// field safely reads a column value from a CSV record, returning "" if the
// index is out of range or the column is not present.
func field(record []string, idx int) string {
	if idx < 0 || idx >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[idx])
}

// ParseCSV reads a CSV file and returns a slice of TargetConfig.
// The CSV must have "hostname", "type", and "ip" column headers.
// Location columns (dc, floor, room, zone) and metadata columns (port, owner, notes)
// are optional. Lines starting with # are treated as comments and skipped.
func ParseCSV(path string) ([]TargetConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.Comment = '#'
	r.TrimLeadingSpace = true

	// Read header row.
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("reading CSV header: %w", err)
	}

	col, err := resolveColumns(header)
	if err != nil {
		return nil, err
	}

	var targets []TargetConfig
	lineNum := 1 // header was line 1
	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading CSV row: %w", err)
		}
		lineNum++

		ip := field(record, col.ip)
		if ip == "" {
			continue // skip empty rows
		}

		hostname := field(record, col.hostname)
		if hostname == "" {
			return nil, fmt.Errorf("line %d: hostname is required", lineNum)
		}

		typ := field(record, col.typ)
		if typ == "" {
			return nil, fmt.Errorf("line %d: type is required", lineNum)
		}

		host, port, err := ParseHostPort(ip)
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid ip %q: %w", lineNum, ip, err)
		}

		// Port column overrides ip:port.
		if p := field(record, col.port); p != "" {
			pn, err := fmt.Sscanf(p, "%d", &port)
			if err != nil || pn != 1 {
				return nil, fmt.Errorf("line %d: invalid port %q", lineNum, p)
			}
		}

		owner := field(record, col.owner)
		if owner == "" {
			owner = OwnerSelf
		}
		if !isValidOwner(owner) {
			return nil, fmt.Errorf("line %d: invalid owner %q (valid: self, isp, colo)", lineNum, owner)
		}

		targets = append(targets, TargetConfig{
			Host: host,
			Port: port,
			Hostname: hostname,
			Type: typ,
			DC: field(record, col.dc),
			Floor: field(record, col.floor),
			Room: field(record, col.room),
			Zone: field(record, col.zone),
			Owner: owner,
			Notes: field(record, col.notes),
		})
	}

	if len(targets) == 0 {
		return nil, fmt.Errorf("CSV file %q contains no targets", path)
	}

	return targets, nil
}

func isValidOwner(owner string) bool {
	for _, v := range ValidOwners {
		if owner == v {
			return true
		}
	}
	return false
}

// RunBatch iterates over all targets in cfg, looks up the producer factory by
// target type, collects the document, and writes it to the hierarchical output
// path: OutputDir/DC/Floor/Room/Zone/Hostname.json.
//
// Failures for individual targets are logged and skipped; the function returns
// nil if at least one target succeeded, or an error if all targets failed.
func RunBatch(cfg *RunConfig, factories FactoryRegistry, logger *slog.Logger) error {
	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	var succeeded, failed int

	for _, target := range cfg.Targets {
		log := logger.With(
			"target", target.Host,
			"hostname", target.Hostname,
			"type", target.Type,
		)

		factory, ok := factories[target.Type]
		if !ok {
			log.Error("unknown producer type", "type", target.Type)
			failed++
			continue
		}

		log.Info("collecting")

		producer := factory(target, cfg)
		ctx := sdk.NewContext(&sdk.ProducerConfig{
			DetailLevel: cfg.DetailLevel,
			SafeFailureMode: cfg.SafeFailureMode,
		})
		ctx.Logger = log

		doc, err := producer.Collect(ctx)
		if err != nil {
			log.Error("collection failed", "error", err)
			failed++
			continue
		}

		data, err := sdk.MarshalDocument(doc)
		if err != nil {
			log.Error("marshal failed", "error", err)
			failed++
			continue
		}

		outPath := OutputPath(cfg.OutputDir, target)
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			log.Error("creating output path", "error", err, "path", outPath)
			failed++
			continue
		}

		if err := os.WriteFile(outPath, data, 0644); err != nil {
			log.Error("write failed", "error", err, "path", outPath)
			failed++
			continue
		}

		log.Info("written", "path", outPath)
		succeeded++
	}

	if succeeded == 0 {
		return fmt.Errorf("all %d targets failed", failed)
	}

	if failed > 0 {
		logger.Warn("batch completed with failures", "succeeded", succeeded, "failed", failed)
	} else {
		logger.Info("batch completed", "succeeded", succeeded)
	}

	return nil
}
