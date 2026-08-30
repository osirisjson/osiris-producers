// transform_security.go - AAA/RADIUS/TACACS+ posture:
// "show aaa accounting"/"show aaa authentication"/"show aaa groups"
// "show radius-server"/"show tacacs-server"->a single osiris.cisco.aaa
// resource, contained by the switch. Shared secrets are never decoded
// (see the DTOs in dto.go), let alone emitted, regardless of purpose.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package nxos

import (
	"fmt"
	"strconv"

	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformAAA converts "show aaa accounting"/"show aaa authentication"
// "show aaa groups"/"show radius-server"/"show tacacs-server" into a
// single custom-typed resource capturing the device's AAA/RADIUS/TACACS
// posture. OSIRIS-JSON-v1.0 section 7.7 has no standard resource type
// for an authentication server or its client-side configuration
// (closest is compute.server, an unsupported claim about hardware this
// producer never queried) per section 7.7.1's own decision tree
// ("vendor-specific resource with no standard equivalent"), this uses
// the namespaced custom type osiris.cisco.aaa, matching the
// osiris.cisco.aci precedent that same section's guideline table names.
// Since the resource's own type is already namespaced, its fields live
// directly in properties (not a further extensions nesting) the same
// shape section 4.4.10 "custom resource with organization extensions"
// example uses for its own vendor-native properties.
//
// Deliberately never decoded, let alone emitted, regardless of purpose:
// TACACS's secretKey and global_testPassword, and RADIUS's server-level
// secret equivalent (that command's per-server table isn't modeled at
// all yet see radiusServerResponse's own doc comment).
//
// Returns the zero Resource and false when none of the five source
// commands produced anything (e.g. all failed/unsupported on this
// platform) callers should skip adding the resource in that case
// rather than emit an empty posture blob.
func TransformAAA(deviceKey string, accounting aaaAccountingResponse, authentication aaaAuthenticationResponse, groups aaaGroupsResponse, radius radiusServerResponse, tacacs tacacsServerResponse) (sdk.Resource, bool) {
	const resType = "osiris.cisco.aaa"
	canonicalKey := deviceKey + "/aaa"
	id := resourceID(providerName, canonicalKey)

	prov := sdk.Provider{
		Name:     providerName,
		NativeID: canonicalKey,
	}

	r, err := sdk.NewResource(id, resType, prov)
	if err != nil {
		return sdk.Resource{}, false
	}
	r.Name = "AAA"
	r.Status = "active"

	props := map[string]any{}

	if len(authentication.TableAuthenMethods.RowAuthenMethods) > 0 {
		var methods []map[string]any
		for _, row := range authentication.TableAuthenMethods.RowAuthenMethods {
			m := map[string]any{}
			if v := string(row.Service); v != "" {
				m["service"] = v
			}
			if v := string(row.Method); v != "" {
				m["method"] = v
			}
			if len(m) > 0 {
				methods = append(methods, m)
			}
		}
		if len(methods) > 0 {
			props["login_methods"] = methods
		}
	}

	if len(accounting.TableAcctMethods.RowAcctMethods) > 0 {
		var methods []map[string]any
		for _, row := range accounting.TableAcctMethods.RowAcctMethods {
			m := map[string]any{}
			if v := string(row.Service); v != "" {
				m["service"] = v
			}
			if v := string(row.Methods); v != "" {
				m["methods"] = v
			}
			if len(m) > 0 {
				methods = append(methods, m)
			}
		}
		if len(methods) > 0 {
			props["accounting_methods"] = methods
		}
	}

	if len(groups.TableGroups.RowGroups) > 0 {
		var names []string
		for _, row := range groups.TableGroups.RowGroups {
			if v := string(row.Group); v != "" {
				names = append(names, v)
			}
		}
		if len(names) > 0 {
			props["server_groups"] = names
		}
	}

	radiusPosture := map[string]any{}
	if v := string(radius.GlobalDeadtime); v != "" {
		radiusPosture["deadtime"] = v
	}
	if v := string(radius.GlobalSecureMode); v != "" {
		radiusPosture["secure_mode"] = v
	}
	if v := string(radius.GlobalSourceIntf); v != "" {
		radiusPosture["source_interface"] = v
	}
	if v := string(radius.GlobalTimeout); v != "" {
		radiusPosture["timeout"] = v
	}
	if v := string(radius.ServerCount); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			radiusPosture["server_count"] = n
		}
	}
	if len(radiusPosture) > 0 {
		props["radius"] = radiusPosture
	}

	tacacsPosture := map[string]any{}
	if v := string(tacacs.GlobalDeadtime); v != "" {
		tacacsPosture["deadtime"] = v
	}
	if v := string(tacacs.GlobalSourceIntf); v != "" {
		tacacsPosture["source_interface"] = v
	}
	if v := string(tacacs.GlobalTimeout); v != "" {
		tacacsPosture["timeout"] = v
	}
	if v := string(tacacs.ServerCount); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			tacacsPosture["server_count"] = n
		}
	}
	if len(tacacsPosture) > 0 {
		props["tacacs"] = tacacsPosture
	}

	if len(tacacs.TableServer.RowServer) > 0 {
		var servers []map[string]any
		for _, row := range tacacs.TableServer.RowServer {
			s := map[string]any{}
			if v := string(row.ServerIP); v != "" {
				s["server_ip"] = v
			}
			if v := string(row.Port); v != "" {
				if n, err := strconv.Atoi(v); err == nil {
					s["port"] = n
				}
			}
			if v := string(row.Timeout); v != "" {
				if n, err := strconv.Atoi(v); err == nil {
					s["timeout"] = n
				}
			}
			if len(s) > 0 {
				servers = append(servers, s)
			}
		}
		if len(servers) > 0 {
			props["tacacs_servers"] = servers
		}
	}

	if len(props) == 0 {
		return sdk.Resource{}, false
	}

	r.Properties = props
	return r, true
}

// TransformAAAContainment builds a "contains" connection from the
// switch to its AAA posture resource the base contains type (not a
// subtype like contains.physical/contains.logical above), since an AAA
// configuration is neither a physical chassis component nor an owned
// interface, just the device's own configuration.
func TransformAAAContainment(deviceID, deviceName string, aaa sdk.Resource) sdk.Connection {
	canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
		Type:      "contains",
		Direction: "forward",
		Source:    deviceID,
		Target:    aaa.ID,
	})
	connID := sdk.BuildConnectionID(canonicalKey, 16)
	conn, _ := sdk.NewConnection(connID, "contains", deviceID, aaa.ID)
	conn.Name = fmt.Sprintf("%s contains %s", deviceName, aaa.Name)
	conn.Direction = "forward"
	conn.Status = "active"
	return conn
}
