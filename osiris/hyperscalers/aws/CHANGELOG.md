# Changelog - Amazon AWS OSIRIS JSON producer

All notable behavioral changes to the **`osirisjson-producer-aws`** producer are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Producer versioning follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

This file tracks the **producer's behavior version** (`metadata.generator.version` in emitted documents).
It is independent of the repository's git tag - a single git tag may bump several producers.
See the root [`CHANGELOG.md`](../../../../CHANGELOG.md) for the release-level index of which producers shipped under each tag.

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
  managed prefix lists (`osiris.aws.prefixlist`), flow logs (`osiris.aws.flowlog`), and
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
  `vpc_peering_connection_id`, and `network_interface_id` when present.
- Managed prefix lists include `max_entries` property.
- Security group ingress and egress rules (including SG-to-SG `UserIdGroupPairs`
  references) captured as `osiris.aws` extensions.
- NACL entries captured as `osiris.aws` extensions.
- Account and VPC group hierarchy; `provider.zone` set for subnets, ENIs, and instances.
- Connection types: `vpc->subnet`, `subnet->route-table`, `sg->eni`, `eni->instance`,
  `vpc->igw`, `subnet->nat`, `eip->nat`, `vpc->nacl`, `vpc->dhcp-options`, `vpc->peering`,
  `tgw->tgw-attachment->subnet`, `tgw->tgw-route-table`, `dx-connection->dx-gateway`,
  `dx-gateway->vif`, `vpn-connection->vpn-gateway`, `vpn-gateway->vpc`,
  `customer-gateway->vpn-connection`.

- **Load balancing, firewall, DNS + acceleration:** ALB/NLB/GWLB
  (`network.loadbalancer`) and Classic ELB (`network.loadbalancer`) with target groups
  (`osiris.aws.targetgroup`), Network Firewall (`network.firewall`), Route53 Resolver
  rules (`osiris.aws.resolver.rule`) and endpoints (`osiris.aws.resolver.endpoint`),
  Route53 hosted zones (`osiris.aws.route53.zone`, global), and Global Accelerators
  (`osiris.aws.globalaccelerator`, global) are collected and emitted.
- Connection types: `lb->subnet`, `lb->security-group`, `lb->target-group`,
  `target-group->instance`, `resolver-endpoint->subnet`, `resolver-endpoint->sg`.

- **Networking completeness:** ELBv2 listeners (`osiris.aws.elbv2.listener`), Direct
  Connect LAGs (`osiris.aws.directconnect.lag`), VPC PrivateLink endpoint service
  configurations (`osiris.aws.vpc.endpointservice`), API Gateway REST APIs
  (`osiris.aws.apigateway.restapi`), API Gateway v2 HTTP/WebSocket APIs
  (`osiris.aws.apigatewayv2.api`), and CloudFront distributions
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
  (`application.service`), and Auto Scaling Groups (`osiris.aws.asg`) are collected
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
  (`application.cache`), and ElastiCache subnet groups
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
  queues (`application.queue`), Kinesis streams (`application.eventstream`), and MSK
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
  systems (`storage.filesystem`), and FSx file systems (`osiris.aws.fsx.filesystem`)
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
  event buses (`osiris.aws.eventbridge.bus`), and Step Functions state machines
  (`osiris.aws.stepfunctions.statemachine`) are collected and emitted.
- SNS topic properties: topic_arn.
- EventBridge bus properties: description.
- Step Functions state machine properties: type (STANDARD or EXPRESS).

- **Extended databases:** DocumentDB clusters (`osiris.aws.docdb.cluster`) and subnet
  groups (`osiris.aws.docdb.subnetgroup`), Neptune clusters (`osiris.aws.neptune.cluster`)
  and subnet groups (`osiris.aws.neptune.subnetgroup`), Redshift clusters
  (`osiris.aws.redshift.cluster`) and subnet groups (`osiris.aws.redshift.subnetgroup`),
  OpenSearch domains (`osiris.aws.opensearch.domain`), and MemoryDB clusters
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
- Connection types: `cluster->security-group`, `cluster->subnet-group`, and
  `subnet-group->subnet` for each engine.

- **Security + identity:** KMS customer-managed keys (`osiris.aws.kms.key`), Secrets
  Manager secrets (`osiris.aws.secretsmanager.secret`), ECR repositories
  (`osiris.aws.ecr.repository`), WAFv2 REGIONAL web ACLs (`osiris.aws.wafv2.webacl`),
  ACM certificates (`osiris.aws.acm.certificate`), and RAM resource shares
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
- CSV batch mode, multi-region (`--region` repeatable), and all-regions
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
[0.1.1]: ../../../CHANGELOG.md#011---2026-06-24
[0.1.0]: ../../../CHANGELOG.md#010---2026-06-22