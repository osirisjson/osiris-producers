# OSIRIS JSON Microsoft Azure producer fixtures

Synthetic Azure CLI responses and the OSIRIS JSON documents the
Microsoft Azure producer (`osiris/hyperscalers/azure`) is expected to
emit from them. `TestGolden` in `golden_test.go` answers every `az`
command the producer issues from these files (no `az` binary, no
network), runs the full collection and transform pipeline with a fixed
clock and compares the emitted document byte for byte with
`purpose/golden_<purpose>.json`. It also runs each subscription twice
to assert identical output and checks that no credential planted in
the inputs is emitted.

## Layout

The OSIRIS JSON producer emits one document per subscription, so a
scenario has one subdirectory per subscription, each exported and
compared on its own:

```text
<scenario>/<subscription>/
  commands.json                 # maps az command lines to az/ files
  az/<command>.json             # JSON as returned by the az CLI
  purpose/
    golden_documentation.json   # expected document at --purpose documentation
    golden_audit.json           # expected document at --purpose audit,
                                  with --include-raw-body
```

`commands.json` holds an ordered list of rules. A rule with `prefix`
matches a command line that starts with it (for example
`network nic list`, or `disk list --resource-group RG-HUB-NVA` for a
per resource group command); a rule with `contains` matches a fragment
of a Resource Graph query or an `az rest` URL. The first matching rule
wins. A rule without a `file`, and any command no rule matches, answers
an empty result (`[]`, `{}` for `show` commands, an empty page for
`graph query` and `rest`).

Every input follows the JSON shape of the az CLI 2.x list and show
commands, including the shapes that differ from the ARM REST API: flat
objects without a `properties` member (`az vmss list`, `az sql mi
list`, `az sql vm list`, `az monitor data-collection rule list`, `az
webapp list`, `az appservice plan list`), `properties: null` from `az
resource list` (filled from one Resource Graph query) and list output
without vault properties (`az keyvault list`, completed by `az keyvault
show`). Member names keep the az CLI casing (`privateIPAddress`,
`diskSizeGB`, `frontendIPConfigurations`). Some references spell an
ARM ID with a different case than the resource itself (resource group
segment, whole ID lower case), as Azure does.

## hub-spoke scenario

A hub-and-spoke landing zone: two subscriptions in one tenant
(`LAB-TENANT`), each exported as its own document, `westeurope`, three
resource groups each. Both subscriptions answer the same tenant-wide
inputs (management group entities, `az account list --all`, ARM tenants
API), so each document names the other subscription without reading it.

`hub/` (`LAB-HUB-SUB`, management group `LAB-MG-PLATFORM`):

- a VNet (`192.0.2.0/24`) with NVA, management, private endpoint, DNS
  resolver inbound (delegated), VMSS, `RouteServerSubnet` and
  `GatewaySubnet` subnets, peered to the spoke VNet with gateway
  transit;
- two NVA VMs (OS disks, NICs with IP forwarding, one with a public IP
  and a NIC-level NSG) behind an internal load balancer whose HA ports
  frontend `192.0.2.10` is the next hop of the management route table;
- a VMSS behind a public load balancer (public IP from a public IP
  prefix), with an NSG and an autoscale setting;
- a Route Server (branch-to-branch enabled, public IP) with three BGP
  peers: both NVA NICs (resolved) and an address no NIC holds
  (unresolved);
- an ExpressRoute gateway, circuit and connection;
- a DNS private resolver and forwarding ruleset;
- monitoring: two Log Analytics workspaces, a data collection rule and
  endpoint, diagnostic settings (VNet, NSG, load balancer, public IP,
  Key Vault, Service Bus, Recovery Services vault, workspace), an NSG
  flow log to the storage account, an action group and metric alerts on
  the load balancer, Route Server, storage account and vault;
- a Key Vault, a storage account, an Automation account reached through
  a private endpoint, a Recovery Services vault protecting one NVA, a
  Service Bus namespace, a Stream Analytics job, a Logic App and an API
  connection.

`spoke/` (`LAB-SPOKE-SUB`, management group `LAB-MG-LANDINGZONES`):

- a VNet (`198.51.100.0/24`) with application, private endpoint, App
  Service integration (delegated), data and SQL managed instance
  (delegated) subnets, peered to the hub VNet using the remote gateway;
- a route table whose routes point at the hub load balancer frontend
  `192.0.2.10`, which is not in the spoke document and stays a next-hop
  IP;
- a VM in an application security group, protected by a Recovery
  Services vault, and a SQL Server VM with its SQL virtual machine
  resource;
- an App Service plan with autoscale and a VNet-integrated web app;
- private endpoints to the web app, a Key Vault, a storage account and
  a container registry;
- a SQL managed instance with one database;
- a user-assigned managed identity, a Data Factory, a Log Analytics
  workspace, a Logic App and an API connection;
- an action group and metric alerts on the web app, the managed
  instance, the storage account and the Key Vault.

Each document carries the other side only as references: a
`network.vpc` stub for the peered VNet with the ARM ID, owning
subscription and tenant of the real VNet, and a `reference_only`
catalog entry for the other subscription. Correlating the two documents
is a consumer task; `TestGoldenHubSpokeIdentityContract` only asserts
that each document, produced alone, carries identifiers that match the
other one (stub and resource IDs, the peering connection ID, the root
management group ID) and that no edge crosses the document boundary.

Synthetic additions not present in the reference export, kept for the
checks they enable:

- the ExpressRoute gateway, circuit and connection (gateway <-> circuit
  edge, connection endpoints, circuit service key never emitted);
- planted credentials that must be redacted: a SAS callback URL on the
  hub action group's Logic App receiver, an Application Insights
  connection string in a hub storage account tag, an API connection
  access key and the ExpressRoute service key;
- the subscription catalog cases in the hub: a Key Vault firewall
  subnet in a second tenant's subscription (named by
  `az account list --all`, its tenant by the ARM tenants API) and a
  storage firewall subnet in a subscription no source names.

The scenario deliberately omit VPN gateways, Azure Firewall, Application
Gateway, AKS, containers, databases other than the SQL managed
instance, and the audit-only per-resource calls other than diagnostic
settings (locks, role assignments, VM extensions, effective routes),
which answer empty.
