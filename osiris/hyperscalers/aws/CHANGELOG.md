# Changelog - Amazon AWS OSIRIS JSON producer

All notable behavioral changes to the **`osirisjson-producer-aws`** producer are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Producer versioning follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

This file tracks the **producer's behavior version** (`metadata.generator.version` in emitted documents).
It is independent of the repository's git tag - a single git tag may bump several producers.
See the root [`CHANGELOG.md`](../../../../CHANGELOG.md) for the release-level index of which producers shipped under each tag.

---

## [0.2.0] - 2026-11-27

### Security

- **Raw bodies are redacted.** With flags `--purpose audit` and
  `--include-raw-body`, the AWS SDK body under
  `extensions["osiris.aws.sdk"].body` is now redacted before it is
  attached: values under sensitive keys, AWS tag elements with a
  sensitive key, secret-shaped strings (also inside embedded or
  URL-encoded JSON) and fields such as `UserData`, `PreSharedKey`,
  `Environment` and policy documents are replaced or dropped.
  A body that still matches a secret pattern is withheld and the
  envelope records the rule name under `rejected`.
- **Raw bodies are built only on request.** Without the flag, or in
  documentation mode, no raw body is serialized, so a secret in an
  unmodelled field can no longer abort a fail-closed run.
- **Final check before writing.** Every document, including the
  empty fallback document, is scanned in its serialized form,
  metadata included. A document with any remaining secret is not
  written.
- **AWS error text is reduced.** `metadata.coverage.errors` and
  stderr diagnostics report AWS errors as service, operation and
  error code. Request IDs, principal ARNs and API messages are no
  longer emitted.
- **Verified caller identity.** Every target is anchored to STS
  `GetCallerIdentity`, tried on the target region and, in the
  commercial partition, once more on `us-east-1`. If no call
  succeeds, no resource is collected and only the empty fallback
  document is written with the coverage error `identity
  unverified`. The CSV `account_id` is no longer used when STS
  fails.
- **Account mismatch is rejected.** When the verified caller
  belongs to a different account than the CSV `account_id`, the
  target fails with `identity mismatch` and no resource is
  collected.
- **Private, atomic output.** Documents are written with mode
  `0600` through a temporary file and published only when
  complete. An existing file, or a symlink, at the destination is
  never overwritten; the target fails instead. Output directories
  are created with mode `0750`.
- **Validated scope input.** Regions from flags, CSV and the
  interactive picker must be AWS region codes and a CSV
  `account_id` must be 12 digits. Target names in file names can
  no longer contain path separators, `..` or control characters.

### Added

- **Account selection.** Flags `-A` / `--account` collects one or more
  comma-separated account profiles without the interactive picker or
  a CSV file. An entry is an AWS CLI profile name or, when only one
  profile matches, the account name without its role suffix (for
  example `account-name` for `account-name_ReadOnlyAccess`). Unknown
  and ambiguous entries and profiles that would write the same file
  name, are rejected before any AWS call. Multiple accounts require
  `--output`; `--profile` and `--source` cannot be combined with it.
- Profile discovery honors `AWS_CONFIG_FILE` and
  `AWS_SHARED_CREDENTIALS_FILE`.
- **Organization and account catalog.** `metadata["osiris.aws"]`
  opens with `organizations`, keyed by organization ID, with
  `management_account_id` and `accounts`, keyed by account ID, with
  `coverage` (`exported` or `reference_only`), `name` and
  `organization_id`, in both purposes.
  The exported account name and the organization are read with
  `account:GetAccountInformation` and
  `organizations:DescribeOrganization`; both are optional and a
  failed read is reported in `metadata.coverage.errors` (resources
  `account name` and `organization`). Unknown values are omitted: AWS
  organizations have no name. Names and membership of referenced
  accounts are read with `organizations:ListAccounts`, which AWS
  allows only from the management account or a delegated
  administrator. Profile or CSV labels are never used as names and
  account emails are not collected. This adds the official
  AWS SDK modules `service/account` and `service/organizations`.
- **Paged core collection.** VPCs, subnets, route tables, network
  interfaces and EC2 instances are read with the AWS SDK
  paginators, up to 200 pages per operation, stopping on a
  repeated token. Previously only the first response was used, so
  large regions could be silently truncated. Items read before a
  failing page are kept.
- **Operation outcomes.** `metadata["osiris.aws"].coverage`
  (schema `operation-outcomes/v1`) records each core operation
  with `outcome` (`succeeded`, `succeeded-empty`, `partial`,
  `budget-limited`, `cancelled`, `denied` or `failed`), `pages`,
  `items` and a reduced `reason`. `core_complete` is `true` only
  when every core operation read all items.
  `unverified_pagination` lists the attempted service families
  whose collectors still read a single response (for `ec2`,
  every operation outside the core set). Their results may be
  incomplete in large accounts.
- **Effective route table associations.** Each subnet gets one
  `osiris.aws.routetable.association` connection (forward, route
  table -> subnet) to its effective route table: the explicitly
  associated table or, when there is none, the VPC main table.
  Audit properties carry `association` (`explicit` or `main`) and,
  for explicit associations, `association_id`. No main-table
  association is inferred when the subnet or route table list was
  incomplete and none is emitted for an ambiguous or
  non-associated state.
- **Instance network interface attachments.** An
  `osiris.aws.eni.attachment` connection (forward, EC2 instance ->
  network interface) is emitted for each interface in the
  `attached` state of a collected instance owned by the caller
  account. Audit properties carry `device_index` and
  `attachment_status`.
- EC2 instances carry the AWS instance state (`running`,
  `stopped`, ...) in `state`.
- Synthetic `core-vpc`, `s3-unverified` and `shared-tgw` source and
  golden fixtures for both purposes.

### Fixed

- The `HttpTokens` instance metadata setting (`required` or
  `optional`) is no longer redacted in raw bodies. Any other value
  under that key is still redacted.
- `--include-raw-body` help no longer states that every resource
  carries a body.
- Properties AWS does not define are omitted instead of emitted
  empty: `scheme` on gateway load balancers, `protocol` on their
  listeners and `protocol` and `port` on Lambda target groups. A
  listener without a protocol is named by its listener ID instead
  of `:0`.
- **Schema status values.** Every status is now one of `active`,
  `inactive`, `degraded`, `retired` or `unknown`. Values outside
  the schema enum were emitted before: `provisioning` and
  `pending` are now `inactive`, `decommissioned` is `retired`,
  `maintenance` is `active` for updating EKS clusters and node
  groups and `inactive` for stopping instances and draining ECS
  services and an unrecognized database status is `unknown`
  instead of the raw AWS value. Any other out-of-schema value is
  replaced with `unknown` before the document is built.
- **S3 security settings are never falsely reported off.** A
  bucket whose versioning, default encryption or public access
  block could not be read (for example access denied) no longer
  reports `versioning: "Disabled"` or `block_public_access: false`.
  The field is omitted and the setting is listed in the audit
  property `unverified_settings`. Only the documented "not
  configured" error codes are reported as off. Buckets with
  unverified settings are counted in `metadata.coverage.errors`
  (resource `S3 bucket settings`).
- **Batch exit status.** A target that fails collection still gets
  its empty fallback document, but it is now counted only as
  failed. A batch in which every target fails exits nonzero.
  Before, fallback documents counted as successes.
- Connections whose endpoint was not collected in the caller
  account no longer produce a fabricated "cross-account" stub. A
  reference stub is emitted only when the endpoint ID carries a
  different, valid owner account taken from AWS data any other
  such connection is dropped and counted in
  `metadata.coverage.errors` (resource `connections`).
- **Shared transit gateways are emitted once.** A transit gateway
  shared to the caller through AWS RAM was keyed by the caller
  account, while its attachments pointed to the owner account, so
  the same gateway appeared twice: once as a collected resource
  without connections and once as a "cross-account" stub. The
  resource is now keyed by the AWS-reported `OwnerId` and carries
  the owner in `provider.account`. Its ID changes a stub is
  emitted only for a gateway the caller cannot describe.
- Constructed resource IDs use the verified caller's partition
  (for example `aws-cn` or `aws-us-gov`) instead of a literal
  `aws`. Commercial-partition IDs are unchanged.
- A malformed RDS cluster ARN no longer causes a panic when
  cluster members are linked.
- The fallback document names an account in `metadata.scope` only
  when it was verified, otherwise it uses `unknown-account`.

### Changed

- `metadata.scope.name` is the account name reported by AWS, else
  the CSV `account_name`, else the account ID. Before, it was the
  account ID, followed by the CSV label when there was one.
- **Breaking, connection migration:** route table to subnet
  connections changed type from `network` to
  `osiris.aws.routetable.association`. Source, target and
  direction are unchanged for explicit associations, but because
  the type is part of the connection identity, their connection
  IDs change. The old `network` route table edge is no longer
  emitted (no duplicate). Consumers that select route table edges
  by `type == "network"` must select the new type.
- **Breaking:** `--safe-failure-mode off` is removed, the accepted
  values are `fail-closed` (default) and `log-and-redact`. The
  fallback document is built with `fail-closed`.
- `log-and-redact` now replaces flagged values in properties,
  extensions, tags, names and descriptions instead of only logging
  them.
- The raw body envelope `schema` is `raw-passthrough/v2`. The body
  is JSON with sorted keys and a space after each comma and colon,
  and `fetched_at` is the run timestamp.
- `--include-raw-body` without `--purpose audit` prints a warning
  that the flag is ignored.
- `metadata.coverage` keeps its earlier meaning for compatibility:
  `services_succeeded` lists families with at least one emitted
  resource, not families read completely. Completeness is reported
  only by `metadata["osiris.aws"].coverage`.
- Go API: `OutputPath` returns `(string, error)` and takes the
  sanitized target name; `CollectedAccountID` returns only a
  verified account; the exported `CrossAccountStubs` and
  `TransformRouteTableToSubnetConnections` are removed in favour of
  `TransformRouteTableAssociations` and
  `TransformInstanceENIAttachments`.
- CLI help and package documentation use the reserved example start
  URL `https://example.awsapps.com/start`.
- The account CSV template and code examples use the approved example
  accounts `123456789012` and `111122223333` and a zero-padded VPC ID.

---

## [0.1.1] - 2026-06-24

### Fixed

- **SSO auto-refresh in single/interactive mode:** `runSingle` now runs the same preflight
  credential check that batch mode runs. When the session is expired, the producer
  automatically triggers `aws sso login` (browser flow) before starting collection instead
  of failing with an error message that required the user to re-authenticate manually.

---

## [0.1.0] - 2026-06-22

### Added

- **Core networking:** VPC (`network.vpc`), Subnet (`network.subnet`), Security Group
  (`network.security.group`), ENI (`network.interface`), Route Table
  (`osiris.aws.routetable`), Internet/NAT/VPN/Customer/Egress-only gateways, Elastic
  IP (`osiris.aws.elasticip`), EC2 instances (`compute.vm`), NACLs (`osiris.aws.nacl`),
  VPC Endpoints (`osiris.aws.vpc.endpoint`), VPC Peering (`osiris.aws.vpc.peering`),
  Transit Gateways + attachments + route tables, Direct Connect connections/gateways/VIFs,
  VPN connections (`osiris.aws.vpn.connection`), DHCP Options (`osiris.aws.dhcpoptions`),
  managed prefix lists (`osiris.aws.prefixlist`), flow logs (`osiris.aws.flowlog`) and
  Availability Zones (`osiris.aws.availabilityzone`) are collected and emitted.
- VPC properties: `cidr_block`, `state`, `enable_dns_hostnames`, `enable_dns_support`
  (last two via `DescribeVpcAttribute`).
- EC2 instance properties: `instance_type`, `private_ip`, `public_ip`, `image_id`,
  `key_name`, `iam_instance_profile_arn`, `metadata_http_tokens`, `metadata_http_endpoint`,
  `block_device_mappings`, `state`.
- VPC endpoint properties include `route_table_ids` and `security_group_ids`; gateway
  endpoints emit `vpc-endpoint->route-table` connections; interface endpoints emit
  `vpc-endpoint->security-group` connections.
- Route table entries include `destination_prefix_list_id`, `destination_ipv6`,
  `vpc_peering_connection_id` and `network_interface_id` when present.
- Managed prefix lists include `max_entries` property.
- Security group ingress and egress rules (including SG-to-SG `UserIdGroupPairs`
  references) captured as `osiris.aws` extensions.
- NACL entries captured as `osiris.aws` extensions.
- Account and VPC group hierarchy; `provider.zone` set for subnets, ENIs and instances.
- Connection types: `vpc->subnet`, `subnet->route-table`, `sg->eni`, `eni->instance`,
  `vpc->igw`, `subnet->nat`, `eip->nat`, `vpc->nacl`, `vpc->dhcp-options`, `vpc->peering`,
  `tgw->tgw-attachment->subnet`, `tgw->tgw-route-table`, `dx-connection->dx-gateway`,
  `dx-gateway->vif`, `vpn-connection->vpn-gateway`, `vpn-gateway->vpc`,
  `customer-gateway->vpn-connection`.

- **Load balancing, firewall, DNS + acceleration:** ALB/NLB/GWLB
  (`network.loadbalancer`) and Classic ELB (`network.loadbalancer`) with target groups
  (`osiris.aws.targetgroup`), Network Firewall (`network.firewall`), Route53 Resolver
  rules (`osiris.aws.resolver.rule`) and endpoints (`osiris.aws.resolver.endpoint`),
  Route53 hosted zones (`osiris.aws.route53.zone`, global) and Global Accelerators
  (`osiris.aws.globalaccelerator`, global) are collected and emitted.
- Connection types: `lb->subnet`, `lb->security-group`, `lb->target-group`,
  `target-group->instance`, `resolver-endpoint->subnet`, `resolver-endpoint->sg`.

- **Networking completeness:** ELBv2 listeners (`osiris.aws.elbv2.listener`), Direct
  Connect LAGs (`osiris.aws.directconnect.lag`), VPC PrivateLink endpoint service
  configurations (`osiris.aws.vpc.endpointservice`), API Gateway REST APIs
  (`osiris.aws.apigateway.restapi`), API Gateway v2 HTTP/WebSocket APIs
  (`osiris.aws.apigatewayv2.api`) and CloudFront distributions
  (`osiris.aws.cloudfront.distribution`, global) are collected and emitted.
- ELBv2 listener properties: protocol, port, ssl_policy, alpn_policy,
  default_action_types.
- Direct Connect LAG properties: connections_bandwidth, number_of_connections,
  minimum_links, location, has_logical_redundancy, encryption_mode.
- VPC endpoint service properties: service_types, acceptance_required,
  manages_vpc_endpoints, private_dns_name, network_load_balancer_arns,
  gateway_load_balancer_arns.
- API Gateway v1 REST API properties: description, endpoint_types,
  disable_execute_api_endpoint, version.
- API Gateway v2 API properties: protocol_type, api_endpoint, description,
  route_selection_expression, version.
- CloudFront distribution properties: domain_name, enabled, http_version, price_class,
  is_ipv6_enabled, comment, origin_count, web_acl_id, alias_count.
- Connection types: `lb->contains->listener`, `lag->contains->dc-connection`,
  `endpoint-service->nlb`.

- **Compute orchestration:** EKS clusters (`osiris.aws.eks.cluster`), EKS node groups
  (`osiris.aws.eks.nodegroup`), ECS clusters (`osiris.aws.ecs.cluster`), ECS services
  (`application.service`) and Auto Scaling Groups (`osiris.aws.asg`) are collected
  and emitted.
- EKS cluster properties: version, endpoint, role ARN, platform version, status,
  logging config, OIDC issuer, subnet IDs, security group IDs, networking config,
  access config.
- EKS node group properties: instance types, capacity type, disk size, AMI type,
  scaling config (min/desired/max), labels, Kubernetes version, status, health issues.
- ECS cluster properties: registered container instances, running/pending task counts,
  active/draining service counts, capacity providers, cluster settings.
- ECS service properties: task definition, desired/running/pending count, launch type,
  platform version/family, deployment config, scheduling strategy, health check grace
  period, enable execute command.
- ASG properties: min/max/desired capacity, cooldown, health check type/grace period,
  instance type, AMI ID, launch template, instance count. ECS services and ASGs derive
  `vpc_id` from subnet membership for correct VPC group placement.
- Connection types: `eks-cluster->subnet`, `eks-cluster->contains->nodegroup`,
  `nodegroup->subnet`, `ecs-cluster->contains->service`, `ecs-service->subnet`,
  `ecs-service->security-group`, `asg->subnet`, `asg->contains->instance`.

- **Managed data:** RDS DB instances (`application.database`), RDS Aurora clusters
  (`osiris.aws.rds.cluster`), RDS DB subnet groups (`osiris.aws.rds.subnetgroup`),
  DynamoDB tables (`application.database`), ElastiCache replication groups
  (`application.cache`) and ElastiCache subnet groups
  (`osiris.aws.elasticache.subnetgroup`) are collected and emitted.
- RDS instance properties: engine/version, instance class, allocated storage, storage
  type, multi-AZ, publicly accessible, deletion protection, backup retention, subnet
  group, availability zone, cluster membership, port, maintenance window.
- RDS cluster properties: engine/version, AZs, multi-AZ, deletion protection, backup
  retention, port, subnet group, member count, maintenance window. Aurora cluster
  members derive `contains` connections by reconstructing member ARNs from the cluster
  member list (only identifiers are embedded in the SDK response).
- RDS subnet group properties: vpc_id, subnet_ids.
- DynamoDB table properties: item count, billing mode, provisioned throughput, key
  schema, GSI/LSI counts, stream enabled.
- ElastiCache replication group properties: cluster_enabled, node_group_count,
  automatic_failover, encryption at rest/in transit, snapshot retention, member IDs.
- ElastiCache subnet group properties: vpc_id, subnet_ids.
- Connection types: `rds-instance->subnet-group`, `rds-instance->security-group`,
  `rds-cluster->subnet-group`, `rds-cluster->security-group`,
  `rds-cluster->contains->rds-instance`, `rds-subnetgroup->subnet`,
  `elasticache-subnetgroup->subnet`.

- **Serverless + messaging:** Lambda functions (`compute.function.serverless`), SQS
  queues (`application.queue`), Kinesis streams (`application.eventstream`) and MSK
  clusters (`osiris.aws.msk.cluster`) are collected and emitted.
- Lambda function properties: runtime, handler, memory_mb, timeout_seconds,
  package_type, architectures, role_arn, ephemeral_storage_mb, vpc config
  (subnet_ids, security_group_ids, vpc_id), description.
- SQS queue properties: queue_type (standard/fifo), visibility_timeout_seconds,
  message_retention_seconds, approximate_message_count, content_based_deduplication,
  dead_letter_target_arn.
- Kinesis stream properties: open_shard_count, retention_period_hours, encryption_type,
  consumer_count.
- MSK cluster properties: broker_node_count, kafka_version, enhanced_monitoring,
  broker_instance_type, subnet_ids, security_group_ids, encryption_in_transit.
  VPC-attached Lambda functions and MSK broker nodes participate in the VPC group
  hierarchy via subnet membership.
- Connection types: `lambda->subnet`, `lambda->security-group`, `msk-cluster->subnet`,
  `msk-cluster->security-group`.

- **Storage:** EBS volumes (`storage.volume`), S3 buckets (`storage.bucket`), EFS file
  systems (`storage.filesystem`) and FSx file systems (`osiris.aws.fsx.filesystem`)
  are collected and emitted.
- EBS volume properties: volume_type, size_gb, encrypted, iops, throughput_mbps,
  availability_zone, multi_attach_enabled, source_snapshot_id, attachments list
  (instance_id, device, delete_on_termination). Name tag used as resource name.
- S3 bucket properties: versioning status. Region resolved via GetBucketLocation
  (one call per bucket); only buckets in the collected region are emitted. Tags
  collected best-effort.
- EFS file system properties: performance_mode, number_of_mount_targets, encrypted,
  throughput_mode, provisioned_throughput_mibps, availability_zone, size_bytes. Mount
  targets collected per file system for subnet wiring.
- FSx file system properties: filesystem_type, storage_capacity_gb, storage_type,
  vpc_id, subnet_ids, dns_name, filesystem_type_version.
- Connection types: `instance->contains->ebs-volume`, `efs->subnet` (via mount target),
  `fsx->subnet`.

- **Event-driven + integration:** SNS topics (`osiris.aws.sns.topic`), EventBridge
  event buses (`osiris.aws.eventbridge.bus`) and Step Functions state machines
  (`osiris.aws.stepfunctions.statemachine`) are collected and emitted.
- SNS topic properties: topic_arn.
- EventBridge bus properties: description.
- Step Functions state machine properties: type (STANDARD or EXPRESS).

- **Extended databases:** DocumentDB clusters (`osiris.aws.docdb.cluster`) and subnet
  groups (`osiris.aws.docdb.subnetgroup`), Neptune clusters (`osiris.aws.neptune.cluster`)
  and subnet groups (`osiris.aws.neptune.subnetgroup`), Redshift clusters
  (`osiris.aws.redshift.cluster`) and subnet groups (`osiris.aws.redshift.subnetgroup`),
  OpenSearch domains (`osiris.aws.opensearch.domain`) and MemoryDB clusters
  (`osiris.aws.memorydb.cluster`) and subnet groups (`osiris.aws.memorydb.subnetgroup`)
  are collected and emitted.
- DocumentDB cluster properties: engine, engine_version, multi_az, port,
  deletion_protection, storage_encrypted, backup_retention_period, master_username.
- Neptune cluster properties: engine, engine_version, multi_az, port, deletion_protection,
  storage_encrypted.
- Redshift cluster properties: node_type, number_of_nodes, cluster_version, db_name,
  master_username, encrypted, publicly_accessible, vpc_id, port.
- OpenSearch domain properties: engine_version, endpoint, subnet_ids, security_group_ids,
  vpc_id, instance_type, instance_count.
- MemoryDB cluster properties: engine, engine_version, node_type, number_of_shards,
  subnet_group_name, port. All subnet group resources carry vpc_id and subnet_ids.
- Connection types: `cluster->security-group`, `cluster->subnet-group` and
  `subnet-group->subnet` for each engine.

- **Security + identity:** KMS customer-managed keys (`osiris.aws.kms.key`), Secrets
  Manager secrets (`osiris.aws.secretsmanager.secret`), ECR repositories
  (`osiris.aws.ecr.repository`), WAFv2 REGIONAL web ACLs (`osiris.aws.wafv2.webacl`),
  ACM certificates (`osiris.aws.acm.certificate`) and RAM resource shares
  (`osiris.aws.ram.resourceshare`) are collected and emitted.
- KMS key properties: description, key_spec, key_usage, encryption_algorithms. Only
  customer-managed keys (KeyManager=CUSTOMER) collected; AWS-managed keys are skipped.
- Secrets Manager secret properties: description, rotation_enabled, rotation_lambda_arn.
  Secret values are never accessed - name and ARN only.
- ECR repository properties: repository_uri, image_tag_mutability, encryption_type,
  scan_on_push.
- WAFv2 web ACL properties: capacity, rule_count, description,
  associated_resource_arns (ARNs of ALBs / API Gateways the ACL protects, from
  ListResourcesForWebACL).

- **Observability + backup:** CloudWatch log groups (`osiris.aws.cloudwatch.loggroup`)
  and AWS Backup vaults (`osiris.aws.backup.vault`) are collected and emitted.
- CloudWatch log group properties: retention_in_days, metric_filter_count, stored_bytes,
  kms_key_id, log_group_class.
- Backup vault properties: number_of_recovery_points, encryption_key_arn, locked,
  min_retention_days, max_retention_days, vault_type.

- **Platform:**
- `--purpose {documentation|audit}` flag controls output fidelity. Producer run default
  in `documentation` mode: `properties` and `extensions` are stripped before emission. Pass
  `--purpose audit` flag for full-fidelity output. Wired through single, CSV, all-regions,
  and interactive modes via `pkg/osirismeta`.
- `--include-raw-body` (requires `--purpose audit`): serialises the full AWS SDK
  response struct for each resource as a JSON string under
  `extensions["osiris.aws.sdk"].body`. Stored as a string (not a nested object) so
  the secret scanner does not recurse into SDK field names containing sensitive
  substrings (e.g. `MasterUsername`).Covered types: VPC, Subnet, SecurityGroup, ENI, 
  RouteTable, EC2 instance, EKS cluster, ECS cluster, ECS service, ASG, RDS instance, 
  RDS cluster, DynamoDB table, ElastiCache replication group, IAM role, KMS key,
  SecretsManager secret, EBS volume, S3 bucket, Lambda function.
- Interactive aws profile picker with range selection syntax (e.g. `1,3,30-55`).
- SSO setup subcommand (`setup-sso`) with OIDC device flow, region auto-detection,
  token caching.
- CSV batch mode, multi-region (`--region` repeatable) and all-regions
  (`--all-regions`) modes.
- Output hierarchy: `amazon-aws-<ts>-<name>/<region>.json` (multi-region) or flat
  file (single region). Global resources (Route53, Global Accelerator) merged into
  the `us-east-1` document.
- `provider.type` uses lowercase kebab-case format (`service:resource-type`, e.g.
  `ec2:vpc`, `lambda:function`, `rds:db-instance`).
- `provider.source` set to `aws-sdk-go-v2`. `metadata.generator.url` set to
  `https://osirisjson.org`.
- `metadata.scope.name` set to "AccountID - AccountName"; `metadata.scope.purpose`
  populated from `--purpose`; `metadata.scope.environments` from CSV `environment`
  column.
- IAM and EC2 permission-denial errors (`UnauthorizedOperation`, `AccessDenied`) are
  logged at `Debug` (expected on restricted SSO permission sets or Organisation SCPs);
  transient and unexpected errors remain at `Warn`.
- Cross-account resource stubs emitted for connection endpoints not reachable via the
  current profile (e.g. shared TGWs, cross-account peering endpoints); stubs carry
  `status: "unknown"` and `state: "cross-account"`.


[Unreleased]: ../../../CHANGELOG.md
[0.2.0]: ../../../CHANGELOG.md#0140---2026-11-27
[0.1.1]: ../../../CHANGELOG.md#011---2026-06-24
[0.1.0]: ../../../CHANGELOG.md#010---2026-06-22