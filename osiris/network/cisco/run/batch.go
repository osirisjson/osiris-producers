// Package run provides CSV parsing and batch orchestration
// for Cisco OSIRIS JSON Producer.
// Provides CSV template generation, target parsing with datacenter
// hierarchy, and a RunBatch function that writes OSIRIS JSON documents
// to a hierarchical directory structure
// (Datacenter/Floor/Room/Rack/Hostname.json).
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification
package run

import (
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// ProducerFactory is a function type that creates a Producer for a
// given target and run configuration.
// Each sub-producer (APIC, NX-OS, IOS-XE) registers its own factory
// that builds the appropriate transport (HTTP or SSH) internally.
type ProducerFactory func(target TargetConfig, cfg *RunConfig) sdk.Producer

// FactoryRegistry maps producer type names to their factory functions.
// Used by RunBatch to dispatch to the correct producer per CSV row.
type FactoryRegistry map[string]ProducerFactory

// csvExampleRows gives CSVTemplate its one example row per producer
// apic gets an APIC controller, nxos an NX-OS switch, iosxe an
// IOS-XE router, each in the context that producer actually collects
// from. Showing the other two producers' rows in, like, an
// "iosxe template --generate" output was confusing (a user running
// iosxe producer has no reason to see apic/nxos examples) one row,
// matching the producer that generated the file, is clearer.
// All template addresses are RFC 5737 compliant.
var csvExampleRows = map[string]string{
	"apic":  "DC-01,F1,R101,RACK-A,apic-01,192.0.2.1,",
	"nxos":  "DC-01,F1,R101,RACK-A,nxos-01,192.0.2.10,",
	"iosxe": "DC-01,F1,R102,RACK-B,iosxe-01,192.0.2.20,",
}

// CSVTemplate returns a CSV template string for batch collection of
// Cisco devices, with a single example row in producerName's own
// context (see csvExampleRows). A batch CSV is inherently
// single-producer execution: which producer's flags/dispatch parsed
// the file already answers "what type is every row"
// (e.g. "osirisjson-producer cisco nxos -s targets.csv" means
// every row in targets.csv is an NX-OS target).
//
// Columns (see OSIRIS-JSON-v1.0 section 7.6.5 for the physical
// containment) levels datacenter/floor/room/rack) map to:
//
//	datacenter    - Datacenter name (optional)
//	floor         - Floor identifier within the datacenter (optional)
//	room          - Room identifier within the floor (optional)
//	rack          - Rack identifier within the room (optional)
//	hostname      - Device label used as output filename (required)
//	management_ip - IP address or FQDN of the target device (required)
//	port          - Override port (optional; default: producer-specific)
//
// datacenter/floor/room/rack are not mandatory, but when present they
// build the output directory structure (see OutputPath).
//
// Credentials apply to every target in the batch, resolved via
// --username and the --secrets-file/environment-variable/interactive
// prompt chain described in flags.go's ParseFlags doc comment there is
// no --password flag. A --secrets-file "rules" document (see
// CredentialFile's doc comment) can supply different credentials per
// target by host/CIDR match without needing a CSV column of its own.
// Output hierarchy: <output-dir>/Datacenter/Floor/Room/Rack/<file>.json
func CSVTemplate(producerName string) string {
	row, ok := csvExampleRows[producerName]
	if !ok {
		// Defensive fallback for a producer name not in csvExampleRows
		// (none exist today - apic/iosxe/nxos are the only callers).
		row = fmt.Sprintf("DC-01,F1,R101,RACK-A,%[1]s-01,192.0.2.1,", producerName)
	}
	return fmt.Sprintf("datacenter,floor,room,rack,hostname,management_ip,port\n%s\n", row)
}

// csvColumns defines the recognized column names and their indices.
type csvColumns struct {
	datacenter int
	floor      int
	room       int
	rack       int
	hostname   int
	ip         int
	port       int
}

// resolveColumns maps header names to column indices.
// Returns an error if required columns (hostname, management_ip) are missing.
func resolveColumns(header []string) (*csvColumns, error) {
	idx := map[string]int{}
	for i, col := range header {
		idx[strings.TrimSpace(strings.ToLower(col))] = i
	}

	col := &csvColumns{
		datacenter: -1, floor: -1, room: -1, rack: -1,
		hostname: -1, ip: -1, port: -1,
	}

	// Required columns.
	var missing []string
	if v, ok := idx["hostname"]; ok {
		col.hostname = v
	} else {
		missing = append(missing, "hostname")
	}
	if v, ok := idx["management_ip"]; ok {
		col.ip = v
	} else {
		missing = append(missing, "management_ip")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("CSV missing required columns: %s", strings.Join(missing, ", "))
	}

	// Optional columns.
	if v, ok := idx["datacenter"]; ok {
		col.datacenter = v
	}
	if v, ok := idx["floor"]; ok {
		col.floor = v
	}
	if v, ok := idx["room"]; ok {
		col.room = v
	}
	if v, ok := idx["rack"]; ok {
		col.rack = v
	}
	if v, ok := idx["port"]; ok {
		col.port = v
	}

	return col, nil
}

// field safely reads a column value from a CSV record, returning ""
// if the index is out of range or the column is not present.
func field(record []string, idx int) string {
	if idx < 0 || idx >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[idx])
}

// ParseCSV parses a CSV file and returns a slice of TargetConfig, every
// one with Type set to producerType a batch CSV never names its own
// producer type (see CSVTemplate's doc comment for why), so the caller
// (ParseFlags, which already knows which producer's flags parsed
// --source) supplies it uniformly for the whole file. The CSV must
// have "hostname" and "management_ip" column headers. Location columns
// (datacenter, floor, room, rack) and the port column are optional.
// Lines starting with # are treated as comments and skipped.
func ParseCSV(path, producerType string) ([]TargetConfig, error) {
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
	lineNum := 1 // header was line 1.
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
			continue // skip empty rows.
		}

		hostname := field(record, col.hostname)
		if hostname == "" {
			return nil, fmt.Errorf("line %d: hostname is required", lineNum)
		}

		host, port, err := ParseHostPort(ip)
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid management_ip %q: %w", lineNum, ip, err)
		}

		// Port column overrides management_ip's own :port suffix.
		// strconv.Atoi (rather than fmt.Sscanf, which silently accepts
		// trailing non-numeric characters after a matched integer) plus
		// an explicit 1-65535 range check, matching ParseHostPort's own
		// validation so the CSV port column cannot bypass it.
		if p := field(record, col.port); p != "" {
			pn, err := strconv.Atoi(p)
			if err != nil || pn < 1 || pn > 65535 {
				return nil, fmt.Errorf("line %d: invalid port %q: must be a base-10 integer 1-65535", lineNum, p)
			}
			port = pn
		}

		targets = append(targets, TargetConfig{
			Host:       host,
			Port:       port,
			Hostname:   hostname,
			Type:       producerType,
			Datacenter: field(record, col.datacenter),
			Floor:      field(record, col.floor),
			Room:       field(record, col.room),
			Rack:       field(record, col.rack),
		})
	}

	if len(targets) == 0 {
		return nil, fmt.Errorf("CSV file %q contains no targets", path)
	}

	return targets, nil
}

// RunBatch executes the batch: for each target in cfg it looks up the
// producer factory by target type, collects the document, and writes it
// to the hierarchical output path:
// OutputDir/Datacenter/Floor/Room/Rack/<file>.json.
//
// Failures for individual targets are logged and skipped; the function
// returns nil if at least one target succeeded,
// or an error if all targets failed.
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
			DetailLevel:     cfg.DetailLevel,
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

		outPath, err := OutputPath(cfg.OutputDir, cfg.Timestamp, target)
		if err != nil {
			log.Error("invalid output path", "error", err)
			failed++
			continue
		}
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			log.Error("creating output path", "error", err, "path", outPath)
			failed++
			continue
		}

		// 0600: emitted documents are infrastructure inventory snapshot
		// (hostnames, serials, topology) and should not be world/group
		// readable by default, only the invoking user.
		if err := os.WriteFile(outPath, data, 0600); err != nil {
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
