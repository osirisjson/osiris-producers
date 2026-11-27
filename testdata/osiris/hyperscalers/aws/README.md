# OSIRIS JSON Amazon AWS producer fixtures

Synthetic AWS SDK data and the OSIRIS JSON documents the Amazon AWS
producer (`osiris/hyperscalers/aws`) is expected to emit from them.
`TestGolden` in `golden_test.go` decodes each scenario's collected data,
runs the full document pipeline (transforms, connection resolution,
groups, secret handling, coverage, purpose projection and the final
publication check) with a fixed clock and compares the emitted document
byte for byte with `purpose/golden_<purpose>.json`. It also runs each
scenario twice to assert identical output, checks that no secret planted
in the inputs is emitted and checks the graph invariants (no dangling
or self connections, schema status values, at most one route table
association per subnet).

## Layout

```text
<scenario>/
  sdk/region-data.json          # collected data of one account and region
  purpose/
    golden_documentation.json   # expected document at --purpose documentation
    golden_audit.json           # expected document at --purpose audit,
                                  with --include-raw-body
```

`sdk/region-data.json` is the JSON form of the producer's `RegionData`:
the caller identity, the caller organization and account name when
known, the region and the AWS SDK for Go v2 types of each
collected list, with the SDK field names (`VpcId`, `Associations`,
`Attachment` and so on). Unknown members are rejected when it is
decoded. The core operations are reported as read completely in one
page.

## Scenarios

| Scenario | Covers |
|---|---|
| `core-vpc` | One VPC with two subnets: one explicitly associated with a route table, one using the VPC main route table. An EC2 instance with an attached network interface and a detached network interface that yields no attachment connection. Planted secrets in VPC tags. |
| `s3-unverified` | Two S3 buckets: one whose versioning, default encryption and public access block were read, one whose reads were denied and are reported only as `unverified_settings`. A planted secret in a bucket tag. |
| `shared-tgw` | A participant account with two VPC attachments: one to a transit gateway shared from another account and visible to the caller, emitted once under its owner account and one to a transit gateway the caller cannot describe, emitted as a cross-account stub. A planted secret in a VPC tag. The caller organization and account name are known, so the account catalog lists the exported account and a listed member with names and the management account without one. |

## Regenerating

After an intended output change:

```bash
OSIRIS_UPDATE_GOLDEN=1 go test ./osiris/hyperscalers/aws/ -run TestGolden
./scripts/validate_golden.sh testdata/osiris/hyperscalers/aws
```

Review the diff of every regenerated file before committing it.
