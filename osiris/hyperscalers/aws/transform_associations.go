// transform_associations.go - VPC configuration bindings for the OSIRIS
// JSON Amazon AWS producer: effective subnet route table associations
// and EC2 instance network interface attachments.
//
// Both relationships describe configuration observed at snapshot time,
// not packet reachability, so they use namespaced vendor connection
// types rather than "network":
//
//   - osiris.aws.routetable.association (forward, route table -> subnet):
//     one edge per subnet for its effective route table. An explicit
//     subnet association wins; otherwise the VPC main route table
//     applies. Audit properties carry association=explicit|main.
//   - osiris.aws.eni.attachment (forward, instance -> network
//     interface): one edge per attached interface of a collected
//     instance. Audit properties carry device_index and
//     attachment_status.
//
// Missing or ambiguous evidence never produces a guessed edge: a
// subnet with two explicit associations, a VPC with no or several main
// tables, an implicit association when the subnet or route table list
// was incomplete, an interface owned through a different account or
// one that is not in the attached state all yield no edge.
//
// For an introduction to OSIRIS JSON Producer for Amazon AWS see:
// [OSIRIS-JSON-AWS]: https://docs.osirisjson.org/osiris-producers/hyperscalers/amazon-aws/

package aws

import (
	"fmt"
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"go.osirisjson.org/producers/pkg/sdk"
)

const (
	routeTableAssociationType = "osiris.aws.routetable.association"
	eniAttachmentType         = "osiris.aws.eni.attachment"
)

// associated reports whether a route table association is in the
// associated state. A nil state is treated as associated, as returned
// by APIs that omit it for established associations.
func associated(a ec2types.RouteTableAssociation) bool {
	return a.AssociationState == nil || a.AssociationState.State == ec2types.RouteTableAssociationStateCodeAssociated
}

// TransformRouteTableAssociations returns one
// osiris.aws.routetable.association connection per subnet whose
// effective route table is known. listsComplete must be true only when
// both the subnet and route table lists were read completely; without
// it no implicit (main table) association is inferred, since an
// explicit association could be on an unread page.
func TransformRouteTableAssociations(rts []ec2types.RouteTable, subnets []ec2types.Subnet, rtIDMap, subnetIDMap map[string]string, listsComplete bool) []sdk.Connection {
	explicit := map[string][]string{} // subnet ID -> route table IDs
	explicitAssoc := map[string]string{}
	mainByVPC := map[string][]string{}
	rtOwner := map[string]string{}
	for _, rt := range rts {
		rtID := aws.ToString(rt.RouteTableId)
		rtOwner[rtID] = aws.ToString(rt.OwnerId)
		for _, a := range rt.Associations {
			if !associated(a) {
				continue
			}
			if aws.ToBool(a.Main) {
				vpc := aws.ToString(rt.VpcId)
				mainByVPC[vpc] = append(mainByVPC[vpc], rtID)
				continue
			}
			if a.SubnetId != nil {
				sid := aws.ToString(a.SubnetId)
				explicit[sid] = append(explicit[sid], rtID)
				explicitAssoc[sid] = aws.ToString(a.RouteTableAssociationId)
			}
		}
	}

	var conns []sdk.Connection
	for _, s := range subnets {
		sid := aws.ToString(s.SubnetId)
		targetID, ok := subnetIDMap[sid]
		if !ok {
			continue
		}
		var rtID, kind string
		switch tables := explicit[sid]; {
		case len(tables) == 1:
			rtID, kind = tables[0], "explicit"
		case len(tables) > 1:
			continue // transitional or ambiguous
		default:
			mains := mainByVPC[aws.ToString(s.VpcId)]
			if !listsComplete || len(mains) != 1 {
				continue
			}
			rtID, kind = mains[0], "main"
		}
		if owner := aws.ToString(s.OwnerId); owner != "" && rtOwner[rtID] != "" && owner != rtOwner[rtID] {
			continue
		}
		sourceID, ok := rtIDMap[rtID]
		if !ok {
			continue
		}
		conn := makeConn(routeTableAssociationType, "forward", sourceID, targetID, fmt.Sprintf("%s -> %s", rtID, sid))
		if conn == nil {
			continue
		}
		props := map[string]any{"association": kind}
		if kind == "explicit" && explicitAssoc[sid] != "" {
			props["association_id"] = explicitAssoc[sid]
		}
		conn.Properties = props
		conns = append(conns, *conn)
	}
	sort.Slice(conns, func(i, j int) bool { return conns[i].ID < conns[j].ID })
	return conns
}

// TransformInstanceENIAttachments returns one osiris.aws.eni.attachment
// connection per network interface attached to a collected instance.
// An attachment is used only when it names an instance, is in the
// attached state and, when the instance owner is reported, that owner
// is accountID.
func TransformInstanceENIAttachments(enis []ec2types.NetworkInterface, instances []ec2types.Instance, eniIDMap map[string]string, region, accountID string) []sdk.Connection {
	known := make(map[string]bool, len(instances))
	for _, inst := range instances {
		known[aws.ToString(inst.InstanceId)] = true
	}
	var conns []sdk.Connection
	for _, eni := range enis {
		att := eni.Attachment
		if att == nil || att.Status != ec2types.AttachmentStatusAttached {
			continue
		}
		instID := aws.ToString(att.InstanceId)
		if instID == "" || !known[instID] {
			continue
		}
		if owner := aws.ToString(att.InstanceOwnerId); owner != "" && owner != accountID {
			continue
		}
		eniNativeID := aws.ToString(eni.NetworkInterfaceId)
		targetID, ok := eniIDMap[eniNativeID]
		if !ok {
			continue
		}
		sourceID := resourceID(accountID, region, "ec2", "instance/"+instID)
		conn := makeConn(eniAttachmentType, "forward", sourceID, targetID, fmt.Sprintf("%s -> %s", instID, eniNativeID))
		if conn == nil {
			continue
		}
		props := map[string]any{"attachment_status": string(att.Status)}
		if att.DeviceIndex != nil {
			props["device_index"] = aws.ToInt32(att.DeviceIndex)
		}
		conn.Properties = props
		conns = append(conns, *conn)
	}
	sort.Slice(conns, func(i, j int) bool { return conns[i].ID < conns[j].ID })
	return conns
}
