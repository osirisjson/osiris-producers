// transform_databases.go - Database resource and connection transforms
// (SQL Server, PostgreSQL, MySQL, Cosmos DB, Redis).
//
// For an introduction to OSIRIS JSON Producer for Microsoft Azure see:
// [OSIRIS-JSON-AZURE]: https://docs.osirisjson.org/osiris-producers/hyperscalers/microsoft-azure/
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package azure

import (
	"fmt"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformSQLServers converts Microsoft.Sql/servers into OSIRIS JSON
// resources of type osiris.azure.sqlserver. Administrator passwords are
// never emitted as per OSIRIS JSON spec chapter 13;
// the login name is treated as non-secret (it is a user principal, not
// authentication material on its own).
func TransformSQLServers(servers []SQLServer, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(servers))

	for _, s := range servers {
		id := resourceID("osiris.azure.sqlserver", s.ID)
		idMap[s.ID] = id

		prov := azureProvider(s.ID, "Microsoft.Sql/servers", s.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.sqlserver", prov)
		if err != nil {
			continue
		}
		r.Name = s.Name
		r.Tags = s.Tags

		props := map[string]any{
			"resource_group": s.ResourceGroup,
		}
		if s.Kind != "" {
			props["kind"] = s.Kind
		}
		ext := map[string]any{}
		if p := s.Properties; p != nil {
			r.Status = mapSQLServerState(p.State)
			r.State = p.State
			if p.Version != "" {
				props["version"] = p.Version
			}
			if p.FullyQualifiedDomainName != "" {
				props["fqdn"] = p.FullyQualifiedDomainName
			}
			if p.AdministratorLogin != "" {
				props["administrator_login"] = p.AdministratorLogin
			}
			if p.PublicNetworkAccess != "" {
				props["public_network_access"] = p.PublicNetworkAccess
				// SQL Server has no networkAcls object; derive default_action from
				// publicNetworkAccess for uniform network policy signalling across types.
				ext["default_action"] = publicNetworkAccessToDefaultAction(p.PublicNetworkAccess)
			}
			if p.MinimalTLSVersion != "" {
				props["min_tls_version"] = p.MinimalTLSVersion
				ext["min_tls_version"] = p.MinimalTLSVersion
			}
			if p.RestrictOutboundNetworkAccess != "" {
				props["restrict_outbound_network_access"] = p.RestrictOutboundNetworkAccess
			}
			if peIDs := collectPEIDs(p.PrivateEndpointConnections); len(peIDs) > 0 {
				ext["private_endpoint_connection_ids"] = peIDs
			}
		} else {
			r.Status = "active"
			r.State = "Succeeded"
		}
		props["database_count"] = len(s.Databases)
		r.Properties = props
		if len(ext) > 0 {
			r.Extensions = map[string]any{extensionNamespace: ext}
		}

		attachArmBody(&r, &s)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformSQLDatabases converts Microsoft.Sql/servers/databases into
// OSIRIS JSON resources of the standard type application.database
// (OSIRIS-JSON-v1.0 section 7.2.1 maps Azure SQL Database directly to
// application.database). The implicit `master` database is skipped at
// collection time.
func TransformSQLDatabases(servers []SQLServer, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string)

	for _, s := range servers {
		for _, db := range s.Databases {
			id := resourceID("application.database", db.ID)
			idMap[db.ID] = id

			prov := azureProvider(db.ID, "Microsoft.Sql/servers/databases", db.Location, sub)

			r, err := sdk.NewResource(id, "application.database", prov)
			if err != nil {
				continue
			}
			r.Name = db.Name
			r.Tags = db.Tags

			props := map[string]any{
				"resource_group": db.ResourceGroup,
				"server_name":    s.Name,
				"server_id":      db.ServerID,
			}
			if db.Kind != "" {
				props["kind"] = db.Kind
			}
			if db.SKU != nil {
				if db.SKU.Name != "" {
					props["sku"] = db.SKU.Name
				}
				if db.SKU.Tier != "" {
					props["tier"] = db.SKU.Tier
				}
				if db.SKU.Capacity > 0 {
					props["capacity"] = db.SKU.Capacity
				}
				if db.SKU.Family != "" {
					props["family"] = db.SKU.Family
				}
			}
			if p := db.Properties; p != nil {
				r.Status = mapSQLDatabaseStatus(p.Status)
				r.State = p.Status
				if p.Collation != "" {
					props["collation"] = p.Collation
				}
				if p.MaxSizeBytes > 0 {
					props["max_size_bytes"] = p.MaxSizeBytes
				}
				if p.ZoneRedundant {
					props["zone_redundant"] = true
				}
				if p.ReadScale != "" {
					props["read_scale"] = p.ReadScale
				}
				if p.StorageAccountType != "" {
					props["storage_account_type"] = p.StorageAccountType
				}
			} else {
				r.Status = "active"
				r.State = "Succeeded"
			}
			r.Properties = props

			attachArmBody(&r, &db)
			resources = append(resources, r)
		}
	}
	return resources, idMap
}

// TransformPostgreSQLServers converts Microsoft.DBforPostgreSQL/flexibleServers
// into OSIRIS JSON resources of the standard type application.database
// a flexible server is itself the running PostgreSQL instance, matching
// OSIRIS-JSON-v1.0 section 7.2.1's "self-hosted databases
// (PostgreSQL...)" use case.
func TransformPostgreSQLServers(servers []PostgreSQLServer, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	return transformFlexServers(
		"application.database",
		"Microsoft.DBforPostgreSQL/flexibleServers",
		flexServerIter(servers),
		sub,
	)
}

// TransformMySQLServers converts Microsoft.DBforMySQL/flexibleServers
// into OSIRIS JSON resources of the standard type application.database
// a flexible server is itself the running MySQL instance, matching
// OSIRIS-JSON-v1.0 section 7.2.1's "self-hosted databases (...MySQL)"
// use case.
func TransformMySQLServers(servers []MySQLServer, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	return transformFlexServers(
		"application.database",
		"Microsoft.DBforMySQL/flexibleServers",
		flexServerIterMySQL(servers),
		sub,
	)
}

// flexServerView is the common read view over PG and MySQL flexible servers
// so the transform logic can be shared.
type flexServerView struct {
	ID            string
	Name          string
	Location      string
	ResourceGroup string
	Tags          map[string]string
	SKU           *azFlexServerSKU
	Properties    *azFlexServerProperties
}

func flexServerIter(servers []PostgreSQLServer) []flexServerView {
	out := make([]flexServerView, len(servers))
	for i, s := range servers {
		out[i] = flexServerView{
			ID: s.ID, Name: s.Name, Location: s.Location,
			ResourceGroup: s.ResourceGroup, Tags: s.Tags,
			SKU: s.SKU, Properties: s.Properties,
		}
	}
	return out
}

func flexServerIterMySQL(servers []MySQLServer) []flexServerView {
	out := make([]flexServerView, len(servers))
	for i, s := range servers {
		out[i] = flexServerView{
			ID: s.ID, Name: s.Name, Location: s.Location,
			ResourceGroup: s.ResourceGroup, Tags: s.Tags,
			SKU: s.SKU, Properties: s.Properties,
		}
	}
	return out
}

func transformFlexServers(osirisType, armType string, views []flexServerView, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(views))

	for _, s := range views {
		id := resourceID(osirisType, s.ID)
		idMap[s.ID] = id

		prov := azureProvider(s.ID, armType, s.Location, sub)

		r, err := sdk.NewResource(id, osirisType, prov)
		if err != nil {
			continue
		}
		r.Name = s.Name
		r.Tags = s.Tags

		props := map[string]any{
			"resource_group": s.ResourceGroup,
		}
		if s.SKU != nil {
			if s.SKU.Name != "" {
				props["sku"] = s.SKU.Name
			}
			if s.SKU.Tier != "" {
				props["tier"] = s.SKU.Tier
			}
		}
		ext := map[string]any{}
		if p := s.Properties; p != nil {
			r.Status = mapFlexServerState(p.State)
			r.State = p.State
			if p.Version != "" {
				props["version"] = p.Version
			}
			if p.AdministratorLogin != "" {
				props["administrator_login"] = p.AdministratorLogin
			}
			if p.FullyQualifiedDomainName != "" {
				props["fqdn"] = p.FullyQualifiedDomainName
			}
			if p.AvailabilityZone != "" {
				props["availability_zone"] = p.AvailabilityZone
			}
			if p.ReplicationRole != "" {
				props["replication_role"] = p.ReplicationRole
			}
			if st := p.Storage; st != nil {
				if st.StorageSizeGB > 0 {
					props["storage_size_gb"] = st.StorageSizeGB
				}
				if st.Tier != "" {
					props["storage_tier"] = st.Tier
				}
				if st.Iops > 0 {
					props["storage_iops"] = st.Iops
				}
				if st.AutoGrow != "" {
					props["storage_auto_grow"] = st.AutoGrow
				}
			}
			if n := p.Network; n != nil {
				if n.PublicNetworkAccess != "" {
					props["public_network_access"] = n.PublicNetworkAccess
				}
				if n.DelegatedSubnetResourceID != "" {
					props["delegated_subnet_id"] = n.DelegatedSubnetResourceID
				}
				if n.PrivateDNSZoneArmResourceID != "" {
					ext["private_dns_zone_id"] = n.PrivateDNSZoneArmResourceID
				}
			}
			if h := p.HighAvailability; h != nil {
				if h.Mode != "" {
					props["ha_mode"] = h.Mode
				}
				if h.StandbyAvailabilityZone != "" {
					props["ha_standby_zone"] = h.StandbyAvailabilityZone
				}
				if h.State != "" {
					props["ha_state"] = h.State
				}
			}
		} else {
			r.Status = "active"
			r.State = "Succeeded"
		}
		r.Properties = props
		if len(ext) > 0 {
			r.Extensions = map[string]any{extensionNamespace: ext}
		}

		attachArmBody(&r, &s)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformCosmosAccounts converts Microsoft.DocumentDB/databaseAccounts
// into OSIRIS JSON resources of the standard type application.database
// (OSIRIS-JSON-v1.0 section 7.2.1 maps CosmosDB directly to
// application.database). Primary/secondary keys and connection strings
// are never collected.
func TransformCosmosAccounts(accts []CosmosAccount, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(accts))

	for _, a := range accts {
		id := resourceID("application.database", a.ID)
		idMap[a.ID] = id

		prov := azureProvider(a.ID, "Microsoft.DocumentDB/databaseAccounts", a.Location, sub)

		r, err := sdk.NewResource(id, "application.database", prov)
		if err != nil {
			continue
		}
		r.Name = a.Name
		r.Tags = a.Tags

		props := map[string]any{
			"resource_group": a.ResourceGroup,
		}
		if a.Kind != "" {
			props["kind"] = a.Kind
		}
		ext := map[string]any{}
		if p := a.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			r.State = p.ProvisioningState
			if p.DatabaseAccountOfferType != "" {
				props["offer_type"] = p.DatabaseAccountOfferType
			}
			if p.DocumentEndpoint != "" {
				props["document_endpoint"] = p.DocumentEndpoint
			}
			if p.PublicNetworkAccess != "" {
				props["public_network_access"] = p.PublicNetworkAccess
			}
			if p.EnableAutomaticFailover {
				props["enable_automatic_failover"] = true
			}
			if p.EnableMultipleWriteLocations {
				props["enable_multiple_write_locations"] = true
			}
			if p.IsVirtualNetworkFilterEnabled {
				props["virtual_network_filter_enabled"] = true
			}
			if p.EnableFreeTier {
				props["enable_free_tier"] = true
			}
			if p.DisableLocalAuth {
				props["entra_only"] = true
			}
			if c := p.ConsistencyPolicy; c != nil && c.DefaultConsistencyLevel != "" {
				props["consistency_level"] = c.DefaultConsistencyLevel
			}
			if locs := flattenCosmosLocations(p.Locations); len(locs) > 0 {
				props["locations"] = locs
			}
			if caps := flattenCosmosCapabilities(p.Capabilities); len(caps) > 0 {
				props["capabilities"] = caps
			}
			if rules := flattenCosmosVNetRules(p.VirtualNetworkRules); len(rules) > 0 {
				ext["virtual_network_rules"] = rules
			}
			if peIDs := collectPEIDs(p.PrivateEndpointConnections); len(peIDs) > 0 {
				ext["private_endpoint_connection_ids"] = peIDs
			}
		} else {
			r.Status = "active"
			r.State = "Succeeded"
		}
		r.Properties = props
		if len(ext) > 0 {
			r.Extensions = map[string]any{extensionNamespace: ext}
		}

		attachArmBody(&r, &a)
		resources = append(resources, r)
	}
	return resources, idMap
}

func flattenCosmosLocations(locs []azCosmosLocation) []map[string]any {
	if len(locs) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(locs))
	for _, l := range locs {
		entry := map[string]any{}
		if l.LocationName != "" {
			entry["name"] = l.LocationName
		}
		entry["failover_priority"] = l.FailoverPriority
		if l.IsZoneRedundant {
			entry["zone_redundant"] = true
		}
		out = append(out, entry)
	}
	return out
}

func flattenCosmosCapabilities(caps []azCosmosCapability) []string {
	if len(caps) == 0 {
		return nil
	}
	out := make([]string, 0, len(caps))
	for _, c := range caps {
		if c.Name != "" {
			out = append(out, c.Name)
		}
	}
	return out
}

func flattenCosmosVNetRules(rules []azCosmosVNetRule) []map[string]any {
	if len(rules) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(rules))
	for _, r := range rules {
		if r.ID == "" {
			continue
		}
		entry := map[string]any{"subnet_id": r.ID}
		if r.IgnoreMissingVNetServiceEndpoint {
			entry["ignore_missing_vnet_service_endpoint"] = true
		}
		out = append(out, entry)
	}
	return out
}

// TransformRedisCaches converts Microsoft.Cache/Redis into OSIRIS JSON
// resources of the standard type application.cache (OSIRIS-JSON-v1.0
// section 7.7.3 maps Microsoft.Cache/redis directly to
// application.cache). Access keys are never collected.
func TransformRedisCaches(caches []RedisCache, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(caches))

	for _, c := range caches {
		id := resourceID("application.cache", c.ID)
		idMap[c.ID] = id

		prov := azureProvider(c.ID, "Microsoft.Cache/Redis", c.Location, sub)
		if len(c.Zones) > 0 {
			prov.Zone = strings.Join(c.Zones, ",")
		}
		r, err := sdk.NewResource(id, "application.cache", prov)
		if err != nil {
			continue
		}
		r.Name = c.Name
		r.Tags = c.Tags
		r.Status = mapProvisioningState(c.ProvisioningState)
		r.State = c.ProvisioningState

		props := map[string]any{
			"resource_group": c.ResourceGroup,
		}
		if c.SKU != nil {
			if c.SKU.Name != "" {
				props["sku"] = c.SKU.Name
			}
			if c.SKU.Family != "" {
				props["family"] = c.SKU.Family
			}
			if c.SKU.Capacity > 0 {
				props["capacity"] = c.SKU.Capacity
			}
		}
		if c.RedisVersion != "" {
			props["redis_version"] = c.RedisVersion
		}
		if c.EnableNonSSLPort {
			props["enable_non_ssl_port"] = true
		}
		if c.MinimumTLSVersion != "" {
			props["minimum_tls_version"] = c.MinimumTLSVersion
		}
		if c.PublicNetworkAccess != "" {
			props["public_network_access"] = c.PublicNetworkAccess
		}
		if c.HostName != "" {
			props["host_name"] = c.HostName
		}
		if c.Port > 0 {
			props["port"] = c.Port
		}
		if c.SSLPort > 0 {
			props["ssl_port"] = c.SSLPort
		}
		if c.ShardCount > 0 {
			props["shard_count"] = c.ShardCount
		}
		if c.ReplicasPerMaster > 0 {
			props["replicas_per_master"] = c.ReplicasPerMaster
		}
		if c.SubnetID != "" {
			props["subnet_id"] = c.SubnetID
		}
		if c.StaticIP != "" {
			props["static_ip"] = c.StaticIP
		}
		if len(c.Zones) > 0 {
			props["zones"] = c.Zones
		}
		r.Properties = props

		ext := map[string]any{}
		if peIDs := collectPEIDs(c.PrivateEndpointConnections); len(peIDs) > 0 {
			ext["private_endpoint_connection_ids"] = peIDs
		}
		if c.MinimumTLSVersion != "" {
			ext["min_tls_version"] = c.MinimumTLSVersion
		}
		if len(ext) > 0 {
			r.Extensions = map[string]any{extensionNamespace: ext}
		}

		attachArmBody(&r, &c)
		resources = append(resources, r)
	}
	return resources, idMap
}

// mapSQLServerState maps SQL server `properties.state` to OSIRIS JSON status.
func mapSQLServerState(state string) string {
	switch strings.ToLower(state) {
	case "ready":
		return "active"
	case "disabled":
		return "inactive"
	case "":
		return "active"
	default:
		return strings.ToLower(state)
	}
}

// mapSQLDatabaseStatus maps SQL database `properties.status` to OSIRIS JSON status.
func mapSQLDatabaseStatus(status string) string {
	switch strings.ToLower(status) {
	case "online":
		return "active"
	case "paused", "pausing", "scaling":
		return "transitioning"
	case "offline", "shutdown":
		return "inactive"
	case "":
		return "active"
	default:
		return strings.ToLower(status)
	}
}

// mapFlexServerState maps PG/MySQL flexible server state to OSIRIS JSON status.
func mapFlexServerState(state string) string {
	switch strings.ToLower(state) {
	case "ready":
		return "active"
	case "stopped", "disabled":
		return "inactive"
	case "starting", "stopping", "updating":
		return "transitioning"
	case "":
		return "active"
	default:
		return strings.ToLower(state)
	}
}

// TransformSQLServerContainsDatabaseConnections wires each SQL Server to its
// child SQL Databases with a `contains` edge.
func TransformSQLServerContainsDatabaseConnections(servers []SQLServer, sqlServerIDMap, sqlDatabaseIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, s := range servers {
		serverID, ok := sqlServerIDMap[s.ID]
		if !ok {
			continue
		}
		for _, db := range s.Databases {
			dbID, ok := sqlDatabaseIDMap[db.ID]
			if !ok {
				continue
			}
			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:      "contains",
				Direction: "forward",
				Source:    serverID,
				Target:    dbID,
			})
			connID := sdk.BuildConnectionID(canonicalKey, 16)
			conn, err := sdk.NewConnection(connID, "contains", serverID, dbID)
			if err != nil {
				continue
			}
			conn.Name = fmt.Sprintf("%s contains %s", s.Name, db.Name)
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformPEToSQLServerConnections wires Private Endpoints to SQL Servers via
// the server's properties.privateEndpointConnections.
func TransformPEToSQLServerConnections(servers []SQLServer, sqlIDMap, peIDMap map[string]string) []sdk.Connection {
	bindings := make([]peBinding, 0, len(servers))
	for _, s := range servers {
		if s.Properties == nil {
			continue
		}
		bindings = append(bindings, peBinding{
			TargetArmID: s.ID,
			Name:        s.Name,
			Conns:       s.Properties.PrivateEndpointConnections,
		})
	}
	return transformPEBoundConnections(bindings, sqlIDMap, peIDMap, "dependency.database")
}

// TransformPEToCosmosAccountConnections wires Private Endpoints to Cosmos DB
// accounts via the account's properties.privateEndpointConnections.
func TransformPEToCosmosAccountConnections(accts []CosmosAccount, cosmosIDMap, peIDMap map[string]string) []sdk.Connection {
	bindings := make([]peBinding, 0, len(accts))
	for _, a := range accts {
		if a.Properties == nil {
			continue
		}
		bindings = append(bindings, peBinding{
			TargetArmID: a.ID,
			Name:        a.Name,
			Conns:       a.Properties.PrivateEndpointConnections,
		})
	}
	return transformPEBoundConnections(bindings, cosmosIDMap, peIDMap, "dependency.database")
}

// TransformPEToRedisConnections wires Private Endpoints to Redis caches.
// Only Premium tier supports Private Link; Basic/Standard return an empty
// privateEndpointConnections slice, which the shared helper silently skips.
func TransformPEToRedisConnections(caches []RedisCache, redisIDMap, peIDMap map[string]string) []sdk.Connection {
	bindings := make([]peBinding, 0, len(caches))
	for _, c := range caches {
		bindings = append(bindings, peBinding{
			TargetArmID: c.ID,
			Name:        c.Name,
			Conns:       c.PrivateEndpointConnections,
		})
	}
	return transformPEBoundConnections(bindings, redisIDMap, peIDMap, "dependency.database")
}

// TransformFlexServerToSubnetConnections emits `network` edges from each
// PG / MySQL flexible server to its delegated subnet (VNet-integrated mode).
// Public-access servers without a delegated subnet are silently skipped.
func TransformFlexServerToSubnetConnections(pgs []PostgreSQLServer, mys []MySQLServer, serverIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	emit := func(serverArmID, serverName, subnetArmID string) {
		if subnetArmID == "" {
			return
		}
		srcID, ok := serverIDMap[serverArmID]
		if !ok {
			return
		}
		dstID, ok := subnetIDMap[subnetArmID]
		if !ok {
			return
		}
		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      "network",
			Direction: "forward",
			Source:    srcID,
			Target:    dstID,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)
		conn, err := sdk.NewConnection(connID, "network", srcID, dstID)
		if err != nil {
			return
		}
		conn.Name = fmt.Sprintf("%s -> %s", serverName, extractLastSegment(subnetArmID))
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	for _, s := range pgs {
		if s.Properties == nil || s.Properties.Network == nil {
			continue
		}
		emit(s.ID, s.Name, s.Properties.Network.DelegatedSubnetResourceID)
	}
	for _, s := range mys {
		if s.Properties == nil || s.Properties.Network == nil {
			continue
		}
		emit(s.ID, s.Name, s.Properties.Network.DelegatedSubnetResourceID)
	}
	return connections
}

// TransformSQLManagedInstances converts Microsoft.Sql/managedInstances into
// OSIRIS JSON resources of type osiris.azure.sqlmi.
func TransformSQLManagedInstances(instances []SQLManagedInstance, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(instances))

	for _, mi := range instances {
		id := resourceID("osiris.azure.sqlmi", mi.ID)
		idMap[mi.ID] = id

		prov := azureProvider(mi.ID, "Microsoft.Sql/managedInstances", mi.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.sqlmi", prov)
		if err != nil {
			continue
		}
		r.Name = mi.Name
		r.Tags = mi.Tags

		props := map[string]any{
			"resource_group": mi.ResourceGroup,
		}
		if mi.SKU != nil {
			if mi.SKU.Name != "" {
				props["sku"] = mi.SKU.Name
			}
			if mi.SKU.Tier != "" {
				props["tier"] = mi.SKU.Tier
			}
			if mi.SKU.Family != "" {
				props["sku_family"] = mi.SKU.Family
			}
			if mi.SKU.Capacity > 0 {
				props["vcores"] = mi.SKU.Capacity
			}
		}

		ext := map[string]any{}
		if p := mi.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			r.State = p.ProvisioningState
			if p.State != "" && p.State != p.ProvisioningState {
				r.State = p.State
				r.Status = mapSQLMIState(p.State)
			}
			if p.SubnetID != "" {
				props["subnet_id"] = p.SubnetID
			}
			if p.VCores > 0 {
				props["vcores"] = p.VCores
			}
			if p.StorageSizeInGB > 0 {
				props["storage_size_gb"] = p.StorageSizeInGB
			}
			if p.LicenseType != "" {
				props["license_type"] = p.LicenseType
			}
			if p.PublicDataEndpointEnabled {
				props["public_data_endpoint_enabled"] = true
			}
			if p.FullyQualifiedDomainName != "" {
				props["fqdn"] = p.FullyQualifiedDomainName
			}
			if p.MinimalTLSVersion != "" {
				ext["min_tls_version"] = p.MinimalTLSVersion
			}
		} else {
			r.Status = "active"
			r.State = "Ready"
		}
		r.Properties = props
		if len(ext) > 0 {
			r.Extensions = map[string]any{extensionNamespace: ext}
		}

		attachArmBody(&r, &mi)
		resources = append(resources, r)
	}
	return resources, idMap
}

func mapSQLMIState(state string) string {
	switch strings.ToLower(state) {
	case "ready":
		return "active"
	case "stopped", "disabled":
		return "inactive"
	case "starting", "stopping", "updating", "creating", "deleting", "inaccessible":
		return "transitioning"
	default:
		return "active"
	}
}

// TransformSQLMIToSubnetConnections wires each SQL Managed Instance to its
// delegated subnet.
func TransformSQLMIToSubnetConnections(instances []SQLManagedInstance, miIDMap, subnetIDMap map[string]string) []sdk.Connection {
	subnetLower := make(map[string]string, len(subnetIDMap))
	for k, v := range subnetIDMap {
		subnetLower[strings.ToLower(k)] = v
	}

	var connections []sdk.Connection
	for _, mi := range instances {
		if mi.Properties == nil || mi.Properties.SubnetID == "" {
			continue
		}
		sourceID, ok := miIDMap[mi.ID]
		if !ok {
			continue
		}
		targetID, ok := subnetLower[strings.ToLower(mi.Properties.SubnetID)]
		if !ok {
			continue
		}
		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      "network",
			Direction: "forward",
			Source:    sourceID,
			Target:    targetID,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)
		conn, err := sdk.NewConnection(connID, "network", sourceID, targetID)
		if err != nil {
			continue
		}
		conn.Name = fmt.Sprintf("%s -> %s", mi.Name, extractLastSegment(mi.Properties.SubnetID))
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// TransformRedisToSubnetConnections emits `network` edges from each Premium
// tier Redis cache to its injected subnet. Basic/Standard caches have no subnet ID and are silently skipped.
func TransformRedisToSubnetConnections(caches []RedisCache, redisIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, c := range caches {
		if c.SubnetID == "" {
			continue
		}
		srcID, ok := redisIDMap[c.ID]
		if !ok {
			continue
		}
		dstID, ok := subnetIDMap[c.SubnetID]
		if !ok {
			continue
		}
		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      "network",
			Direction: "forward",
			Source:    srcID,
			Target:    dstID,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)
		conn, err := sdk.NewConnection(connID, "network", srcID, dstID)
		if err != nil {
			continue
		}
		conn.Name = fmt.Sprintf("%s -> %s", c.Name, extractLastSegment(c.SubnetID))
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// TransformSQLMIDatabases converts SQL Managed Instance databases into
// OSIRIS JSON resources of the standard type application.database same
// reasoning as TransformSQLDatabases (OSIRIS-JSON-v1.0 section 7.2.1):
// the database itself, not the managed instance that hosts it, is
// the data-holding resource.
func TransformSQLMIDatabases(instances []SQLManagedInstance, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string)

	for _, mi := range instances {
		for _, db := range mi.Databases {
			id := resourceID("application.database", db.ID)
			idMap[db.ID] = id

			prov := azureProvider(db.ID, "Microsoft.Sql/managedInstances/databases", db.Location, sub)
			r, err := sdk.NewResource(id, "application.database", prov)
			if err != nil {
				continue
			}
			r.Name = db.Name
			r.Tags = db.Tags

			props := map[string]any{
				"resource_group":      db.ResourceGroup,
				"managed_instance_id": mi.ID,
			}
			if p := db.Properties; p != nil {
				r.Status = mapProvisioningState(p.ProvisioningState)
				r.State = p.ProvisioningState
				if p.Collation != "" {
					props["collation"] = p.Collation
				}
			} else {
				r.Status = "active"
				r.State = "Online"
			}
			r.Properties = props
			resources = append(resources, r)
		}
	}
	return resources, idMap
}

// TransformSQLMIContainsDatabaseConnections wires each MI to its databases.
func TransformSQLMIContainsDatabaseConnections(instances []SQLManagedInstance, miIDMap, dbIDMap map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, mi := range instances {
		srcID, ok := miIDMap[mi.ID]
		if !ok {
			continue
		}
		for _, db := range mi.Databases {
			dstID, ok := dbIDMap[db.ID]
			if !ok {
				continue
			}
			key := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{Type: "contains", Direction: "forward", Source: srcID, Target: dstID})
			connID := sdk.BuildConnectionID(key, 16)
			conn, err := sdk.NewConnection(connID, "contains", srcID, dstID)
			if err != nil {
				continue
			}
			conn.Name = fmt.Sprintf("%s -> %s", mi.Name, db.Name)
			_ = conn.SetDirection("forward")
			conns = append(conns, conn)
		}
	}
	return conns
}

// TransformSQLElasticPools converts SQL elastic pools into OSIRIS JSON resources.
func TransformSQLElasticPools(servers []SQLServer, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string)

	for _, s := range servers {
		for _, ep := range s.ElasticPools {
			id := resourceID("osiris.azure.sql.elasticpool", ep.ID)
			idMap[ep.ID] = id

			prov := azureProvider(ep.ID, "Microsoft.Sql/servers/elasticPools", ep.Location, sub)
			r, err := sdk.NewResource(id, "osiris.azure.sql.elasticpool", prov)
			if err != nil {
				continue
			}
			r.Name = ep.Name
			r.Tags = ep.Tags

			props := map[string]any{
				"resource_group": ep.ResourceGroup,
				"server_id":      s.ID,
			}
			if ep.SKU != nil {
				if ep.SKU.Name != "" {
					props["sku"] = ep.SKU.Name
				}
				if ep.SKU.Tier != "" {
					props["tier"] = ep.SKU.Tier
				}
				if ep.SKU.Capacity > 0 {
					props["capacity"] = ep.SKU.Capacity
				}
			}
			if p := ep.Properties; p != nil {
				r.Status = mapProvisioningState(p.ProvisioningState)
				r.State = p.ProvisioningState
				if p.MaxSizeBytes > 0 {
					props["max_size_bytes"] = p.MaxSizeBytes
				}
				if p.ZoneRedundant {
					props["zone_redundant"] = true
				}
			} else {
				r.Status = "active"
				r.State = "Ready"
			}
			r.Properties = props
			resources = append(resources, r)
		}
	}
	return resources, idMap
}

// TransformSQLServerContainsElasticPoolConnections wires each SQL server to its elastic pools.
func TransformSQLServerContainsElasticPoolConnections(servers []SQLServer, serverIDMap, poolIDMap map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, s := range servers {
		srcID, ok := serverIDMap[s.ID]
		if !ok {
			continue
		}
		for _, ep := range s.ElasticPools {
			dstID, ok := poolIDMap[ep.ID]
			if !ok {
				continue
			}
			key := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{Type: "contains", Direction: "forward", Source: srcID, Target: dstID})
			connID := sdk.BuildConnectionID(key, 16)
			conn, err := sdk.NewConnection(connID, "contains", srcID, dstID)
			if err != nil {
				continue
			}
			conn.Name = fmt.Sprintf("%s -> %s", s.Name, ep.Name)
			_ = conn.SetDirection("forward")
			conns = append(conns, conn)
		}
	}
	return conns
}

// TransformSQLVMs converts SQL Server on Azure VMs into OSIRIS JSON resources.
func TransformSQLVMs(vms []SQLVirtualMachine, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(vms))

	for _, v := range vms {
		id := resourceID("osiris.azure.sqlvm", v.ID)
		idMap[v.ID] = id

		prov := azureProvider(v.ID, "Microsoft.SqlVirtualMachine/SqlVirtualMachines", v.Location, sub)
		r, err := sdk.NewResource(id, "osiris.azure.sqlvm", prov)
		if err != nil {
			continue
		}
		r.Name = v.Name
		r.Tags = v.Tags

		props := map[string]any{"resource_group": v.ResourceGroup}
		if p := v.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			r.State = p.ProvisioningState
			if p.SQLServerLicenseType != "" {
				props["license_type"] = p.SQLServerLicenseType
			}
			if p.SQLManagementType != "" {
				props["management_type"] = p.SQLManagementType
			}
			if p.SQLImageSKU != "" {
				props["image_sku"] = p.SQLImageSKU
			}
			if p.VirtualMachineID != "" {
				props["vm_id"] = p.VirtualMachineID
			}
		} else {
			r.Status = "active"
			r.State = "Succeeded"
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformSQLVMToVMConnections wires each SQL VM to its underlying Azure VM.
func TransformSQLVMToVMConnections(sqlVMs []SQLVirtualMachine, sqlVMIDMap, vmIDMap map[string]string) []sdk.Connection {
	vmLower := make(map[string]string, len(vmIDMap))
	for k, v := range vmIDMap {
		vmLower[strings.ToLower(k)] = v
	}

	var conns []sdk.Connection
	for _, v := range sqlVMs {
		if v.Properties == nil || v.Properties.VirtualMachineID == "" {
			continue
		}
		srcID, ok := sqlVMIDMap[v.ID]
		if !ok {
			continue
		}
		dstID, ok := vmLower[strings.ToLower(v.Properties.VirtualMachineID)]
		if !ok {
			continue
		}
		key := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{Type: "dependency", Direction: "forward", Source: srcID, Target: dstID})
		connID := sdk.BuildConnectionID(key, 16)
		conn, err := sdk.NewConnection(connID, "dependency", srcID, dstID)
		if err != nil {
			continue
		}
		conn.Name = fmt.Sprintf("%s -> %s", v.Name, extractLastSegment(v.Properties.VirtualMachineID))
		_ = conn.SetDirection("forward")
		conns = append(conns, conn)
	}
	return conns
}
