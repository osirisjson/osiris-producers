# The OSIRIS JSON Producers

[![GitHub license](https://img.shields.io/badge/license-Apache2.0-blue.svg)](https://raw.githubusercontent.com/osirisjson/osiris-producers/master/LICENSE)
[![Go Reference](https://pkg.go.dev/badge/github.com/osirisjson/osiris-producers.svg)](https://pkg.go.dev/go.osirisjson.org/producers)
[![Security Rating](https://sonarcloud.io/api/project_badges/measure?project=osirisjson_osiris-producers&metric=security_rating&token=341158228074a961caa8700b5b8ab74f7ac963b2)](https://sonarcloud.io/summary/new_code?id=osirisjson_osiris-producers)


An **OSIRIS JSON Producer** is a lightweight, read-only program written in Go that runs on your workstation or server. It connects directly to supported infrastructure environments, discovers resources and relationships, normalizes vendor-specific data, redacts sensitive values, and generates a portable OSIRIS JSON document representing the infrastructure at a specific point in time.

![The OSIRIS JSON producer for AWS](.github/media/osiris-json-demo-aws.gif)

*In this demo it's connecting to AWS account and generating an OSIRIS JSON document*


### The OSIRIS JSON Manifesto in practice

End-to-end infrastructure visibility should not depend on tribal knowledge, expensive consultancy, or closed and proprietary external platforms.

When you run an OSIRIS JSON Producer:

* No AI platform, MCP server, or external agent is required.
* No SaaS subscription or intermediary service is required.
* The OSIRIS JSON producer runs within an environment you fully control.
* Credentials and authentication material are excluded from the generated document.
* Operational logs provide clear visibility into discovery, normalization, redaction, and emission.

*The generated OSIRIS JSON document remains local unless you decide to move, integrate, or share it. **You alone determine how it is used**.*

[Read the OSIRIS JSON Manifesto](https://osirisjson.org/en/commitments/manifesto)

---

### Architecture workflow

```mermaid
flowchart TD 
    subgraph VP ["Vendor / Platform"]
        A["Hyperscalers, Cloud & Hosting Providers"]
        B["On-Premises<br/>IT and OT"]
    end
    
    subgraph SP ["The OSIRIS JSON Specification"]
        S["The authoritative<br/>source of truth for the<br/>specification and core schema"]
    end
    
    subgraph PR ["The OSIRIS JSON Producers"]
        P["Discover, Normalize, Redact, Emit"]
        J["OSIRIS JSON Document<br/>Portable point-in-time infrastructure snapshot"]
    end
    
    subgraph VA ["The OSIRIS JSON Toolbox"]
        V["Validation toolbox<br/>Three-level validation"]
    end
    
    subgraph OC ["The OSIRIS JSON Consumers"]
        C["Consumers under development<br/>Read OSIRIS JSON documents"]
        AD["Architecture documentation reports"]
        AU["Audit reports"]
        DF["Snapshot diff view"]
        TP["Automatic topology<br/>generation and updates"]
    end
    
    subgraph UC ["Under Your Full Control"]
        U["One OSIRIS JSON file<br/>Multiple use cases"]
        M["CMDB / IPAM / DCIM workflows"]
        AI["AI platforms, agents<br/>and MCP servers"]
        X["Custom analytics<br/>and third-party tools"]
    end
    
    A -->|"Discovered by"| P
    B -->|"Discovered by"| P

    S -. "Defines the format" .-> J
    S -. "Guides implementation" .-> P

    P -->|"Generates"| J

    J -->|"Validated by"| V

    J -->|"Consumed by"| C
    C -->|"Generates"| AD
    C -->|"Generates"| AU
    C -->|"Generates"| DF
    C -->|"Generates"| TP

    J --> U
    U --> M
    U --> AI
    U --> X
    
    style VP fill:transparent,stroke:#555,stroke-width:1px,color:inherit
    style SP fill:transparent,stroke:#555,stroke-width:1px,color:inherit
    style PR fill:transparent,stroke:#4F7FE7,stroke-width:2px,color:inherit
    style VA fill:transparent,stroke:#555,stroke-width:1px,color:inherit
    style OC fill:transparent,stroke:#555,stroke-width:1px,color:inherit
    style UC fill:transparent,stroke:#555,stroke-width:1px,color:inherit
```

---

### Available producers

| OSIRIS JSON Producer | Category | Status |
|---|---|---|
| **Amazon AWS** | Hyperscaler | [Available](https://docs.osirisjson.org/osiris-producers/hyperscalers/amazon-aws/) |
| **Microsoft Azure** | Hyperscaler | [Available](https://docs.osirisjson.org/osiris-producers/hyperscalers/microsoft-azure/) |
| **Google Cloud** | Hyperscaler | [Under development](https://osirisjson.org/en/commitments/roadmap) |
| **HPE Aruba Central** | Network | [Available](https://docs.osirisjson.org/osiris-producers/network/hpe-aruba-networking-central) |
| **Cisco** | Network | [Available](https://docs.osirisjson.org/osiris-producers/network/cisco/) |
| **Cloudflare** | Hyperscaler | Planned |
| **Proxmox VE** | Hypervisor | [Under development](https://osirisjson.org/en/commitments/roadmap) |

*For the complete roadmap, visit [osirisjson.org/en/commitments/roadmap](https://osirisjson.org/en/commitments/roadmap).*

### Installation

See the official guidelines available on [docs.osirisjson.org/osiris-producers/installation](https://docs.osirisjson.org/osiris-producers/installation/)

### Documentation and contribution

- [OSIRIS JSON Producer Development Guidelines](https://docs.osirisjson.org/developer-guidelines/producers/producer-guidelines/)
- [Community & contribution guidelines](https://docs.osirisjson.org/community/contributing/)