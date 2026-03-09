package policy

import (
	"sort"
	"strconv"
	"strings"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/cilium/cilium/pkg/policy/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/SoulKyu/cpg/pkg/labels"
)

// ReservedWorldIdentity is the Cilium reserved identity for external/world traffic.
const ReservedWorldIdentity uint32 = 2

// reservedEntityMap maps Cilium reserved label values to Cilium policy entities.
var reservedEntityMap = map[string]api.Entity{
	"reserved:kube-apiserver": api.EntityKubeAPIServer,
	"reserved:host":          api.EntityHost,
	"reserved:remote-node":   api.EntityRemoteNode,
	"reserved:health":        api.EntityHealth,
}

// isWorldIdentity returns true if the endpoint represents external (world) traffic.
func isWorldIdentity(ep *flowpb.Endpoint) bool {
	if ep == nil {
		return false
	}
	if ep.Identity == ReservedWorldIdentity {
		return true
	}
	for _, l := range ep.Labels {
		if l == "reserved:world" {
			return true
		}
	}
	return false
}

// reservedEntity returns the Cilium entity for a reserved endpoint, or empty string.
func reservedEntity(ep *flowpb.Endpoint) api.Entity {
	if ep == nil {
		return ""
	}
	for _, l := range ep.Labels {
		if e, ok := reservedEntityMap[l]; ok {
			return e
		}
	}
	return ""
}

// getSourceIP extracts the source IP from a flow's IP layer (nil-safe).
func getSourceIP(f *flowpb.Flow) string {
	if f.IP == nil {
		return ""
	}
	return f.IP.Source
}

// getDestinationIP extracts the destination IP from a flow's IP layer (nil-safe).
func getDestinationIP(f *flowpb.Flow) string {
	if f.IP == nil {
		return ""
	}
	return f.IP.Destination
}

// PolicyEvent wraps a generated CiliumNetworkPolicy with its target location.
type PolicyEvent struct {
	Namespace string
	Workload  string
	Policy    *ciliumv2.CiliumNetworkPolicy
}

// BuildPolicy transforms a set of Hubble dropped flows into a CiliumNetworkPolicy.
// For INGRESS flows: endpointSelector = destination, IngressRule with FromEndpoints = source.
// For EGRESS flows: endpointSelector = source, EgressRule with ToEndpoints = destination.
func BuildPolicy(namespace, workload string, flows []*flowpb.Flow) *ciliumv2.CiliumNetworkPolicy {
	cnp := &ciliumv2.CiliumNetworkPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "cilium.io/v2",
			Kind:       "CiliumNetworkPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cpg-" + workload,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "cpg",
			},
		},
		Spec: &api.Rule{},
	}

	// Group flows by direction
	var ingressFlows, egressFlows []*flowpb.Flow
	for _, f := range flows {
		if f.L4 == nil {
			continue
		}
		switch f.TrafficDirection {
		case flowpb.TrafficDirection_INGRESS:
			ingressFlows = append(ingressFlows, f)
		case flowpb.TrafficDirection_EGRESS:
			egressFlows = append(egressFlows, f)
		}
	}

	// Set endpointSelector from the first flow that has labels
	if len(flows) > 0 {
		var selectorLabels []string
		for _, f := range flows {
			if f.TrafficDirection == flowpb.TrafficDirection_INGRESS && f.Destination != nil {
				selectorLabels = f.Destination.Labels
				break
			}
			if f.TrafficDirection == flowpb.TrafficDirection_EGRESS && f.Source != nil {
				selectorLabels = f.Source.Labels
				break
			}
		}
		if selectorLabels != nil {
			cnp.Spec.EndpointSelector = labels.BuildEndpointSelector(selectorLabels)
		}
	}

	// Build ingress rules: group by peer (source labels)
	cnp.Spec.Ingress = buildIngressRules(ingressFlows, namespace)

	// Build egress rules: group by peer (destination labels)
	cnp.Spec.Egress = buildEgressRules(egressFlows, namespace)

	return cnp
}

// peerKey creates a deterministic string key from peer labels for grouping.
func peerKey(peerLabels []string) string {
	selected := labels.SelectLabels(peerLabels)
	keys := make([]string, 0, len(selected))
	for k := range selected {
		keys = append(keys, k+"="+selected[k])
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

// flowProto describes the protocol extracted from a flow's L4 layer.
type flowProto struct {
	port  string       // port number for TCP/UDP, ICMP type number for ICMP
	proto api.L4Proto  // ProtoTCP, ProtoUDP, ProtoICMP, or ProtoICMPv6
	icmp  bool         // true if this is an ICMP flow
}

// extractProto extracts protocol information from a flow's L4 layer.
// Returns nil if L4 is nil or has no recognized protocol.
func extractProto(f *flowpb.Flow) *flowProto {
	if f.L4 == nil {
		return nil
	}
	if tcp := f.L4.GetTCP(); tcp != nil {
		return &flowProto{
			port:  strconv.FormatUint(uint64(tcp.DestinationPort), 10),
			proto: api.ProtoTCP,
		}
	}
	if udp := f.L4.GetUDP(); udp != nil {
		return &flowProto{
			port:  strconv.FormatUint(uint64(udp.DestinationPort), 10),
			proto: api.ProtoUDP,
		}
	}
	if icmp4 := f.L4.GetICMPv4(); icmp4 != nil {
		return &flowProto{
			port:  strconv.FormatUint(uint64(icmp4.Type), 10),
			proto: api.ProtoICMP,
			icmp:  true,
		}
	}
	if icmp6 := f.L4.GetICMPv6(); icmp6 != nil {
		return &flowProto{
			port:  strconv.FormatUint(uint64(icmp6.Type), 10),
			proto: api.ProtoICMPv6,
			icmp:  true,
		}
	}
	return nil
}

// icmpFamily returns the ICMP family string for a protocol.
func icmpFamily(proto api.L4Proto) string {
	if proto == api.ProtoICMPv6 {
		return api.IPv6Family
	}
	return api.IPv4Family
}

// peerRules collects ports and ICMP types for a peer grouping.
type peerRules struct {
	ports    []api.PortProtocol
	icmpFields []api.ICMPField
	seen     map[string]struct{}
}

func (pr *peerRules) addFlow(fp *flowProto) {
	dedupKey := fp.port + "/" + string(fp.proto)
	if _, dup := pr.seen[dedupKey]; dup {
		return
	}
	pr.seen[dedupKey] = struct{}{}
	if fp.icmp {
		icmpType := intstr.FromInt32(int32(mustAtoi(fp.port)))
		pr.icmpFields = append(pr.icmpFields, api.ICMPField{
			Family: icmpFamily(fp.proto),
			Type:   &icmpType,
		})
	} else {
		pr.ports = append(pr.ports, api.PortProtocol{
			Port:     fp.port,
			Protocol: fp.proto,
		})
	}
}

func newPeerRules() *peerRules {
	return &peerRules{seen: make(map[string]struct{})}
}

func mustAtoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// buildIngressRules groups ingress flows by source peer and builds IngressRules.
// Handles endpoint selectors, CIDR (world), reserved entities, and ICMP.
func buildIngressRules(flows []*flowpb.Flow, policyNamespace string) []api.IngressRule {
	peers := make(map[string]*struct {
		selector api.EndpointSelector
		*peerRules
	})
	cidrs := make(map[string]*struct {
		cidr api.CIDR
		*peerRules
	})
	entities := make(map[api.Entity]*peerRules)
	var peerOrder, cidrOrder []string
	var entityOrder []api.Entity

	for _, f := range flows {
		if f.Source == nil {
			continue
		}
		fp := extractProto(f)
		if fp == nil {
			continue
		}

		// Reserved entities (kube-apiserver, host, remote-node, etc.)
		if entity := reservedEntity(f.Source); entity != "" {
			er, exists := entities[entity]
			if !exists {
				er = newPeerRules()
				entities[entity] = er
				entityOrder = append(entityOrder, entity)
			}
			er.addFlow(fp)
			continue
		}

		// World identity → CIDR rule
		if isWorldIdentity(f.Source) {
			ip := getSourceIP(f)
			if ip == "" {
				continue
			}
			cidrStr := ip + "/32"
			cp, exists := cidrs[cidrStr]
			if !exists {
				cp = &struct {
					cidr api.CIDR
					*peerRules
				}{cidr: api.CIDR(cidrStr), peerRules: newPeerRules()}
				cidrs[cidrStr] = cp
				cidrOrder = append(cidrOrder, cidrStr)
			}
			cp.addFlow(fp)
			continue
		}

		// Regular endpoint
		key := peerKey(f.Source.Labels)
		pp, exists := peers[key]
		if !exists {
			pp = &struct {
				selector api.EndpointSelector
				*peerRules
			}{
				selector:  labels.BuildPeerSelector(f.Source.Labels, f.Source.Namespace, policyNamespace),
				peerRules: newPeerRules(),
			}
			peers[key] = pp
			peerOrder = append(peerOrder, key)
		}
		pp.addFlow(fp)
	}

	var rules []api.IngressRule

	// Entity rules — ICMPs and ToPorts must be in separate rules per Cilium spec
	for _, entity := range entityOrder {
		er := entities[entity]
		common := api.IngressCommonRule{FromEntities: api.EntitySlice{entity}}
		if len(er.ports) > 0 {
			rules = append(rules, api.IngressRule{
				IngressCommonRule: common,
				ToPorts:           api.PortRules{{Ports: er.ports}},
			})
		}
		if len(er.icmpFields) > 0 {
			rules = append(rules, api.IngressRule{
				IngressCommonRule: common,
				ICMPs:             api.ICMPRules{{Fields: er.icmpFields}},
			})
		}
	}

	// CIDR rules
	for _, key := range cidrOrder {
		cp := cidrs[key]
		common := api.IngressCommonRule{FromCIDR: api.CIDRSlice{cp.cidr}}
		if len(cp.ports) > 0 {
			rules = append(rules, api.IngressRule{
				IngressCommonRule: common,
				ToPorts:           api.PortRules{{Ports: cp.ports}},
			})
		}
		if len(cp.icmpFields) > 0 {
			rules = append(rules, api.IngressRule{
				IngressCommonRule: common,
				ICMPs:             api.ICMPRules{{Fields: cp.icmpFields}},
			})
		}
	}

	// Endpoint selector rules
	for _, key := range peerOrder {
		pp := peers[key]
		common := api.IngressCommonRule{FromEndpoints: []api.EndpointSelector{pp.selector}}
		if len(pp.ports) > 0 {
			rules = append(rules, api.IngressRule{
				IngressCommonRule: common,
				ToPorts:           api.PortRules{{Ports: pp.ports}},
			})
		}
		if len(pp.icmpFields) > 0 {
			rules = append(rules, api.IngressRule{
				IngressCommonRule: common,
				ICMPs:             api.ICMPRules{{Fields: pp.icmpFields}},
			})
		}
	}
	return rules
}

// buildEgressRules groups egress flows by destination peer and builds EgressRules.
// Handles endpoint selectors, CIDR (world), reserved entities, and ICMP.
func buildEgressRules(flows []*flowpb.Flow, policyNamespace string) []api.EgressRule {
	peers := make(map[string]*struct {
		selector api.EndpointSelector
		*peerRules
	})
	cidrs := make(map[string]*struct {
		cidr api.CIDR
		*peerRules
	})
	entities := make(map[api.Entity]*peerRules)
	var peerOrder, cidrOrder []string
	var entityOrder []api.Entity

	for _, f := range flows {
		if f.Destination == nil {
			continue
		}
		fp := extractProto(f)
		if fp == nil {
			continue
		}

		// Reserved entities (kube-apiserver, host, remote-node, etc.)
		if entity := reservedEntity(f.Destination); entity != "" {
			er, exists := entities[entity]
			if !exists {
				er = newPeerRules()
				entities[entity] = er
				entityOrder = append(entityOrder, entity)
			}
			er.addFlow(fp)
			continue
		}

		// World identity → CIDR rule
		if isWorldIdentity(f.Destination) {
			ip := getDestinationIP(f)
			if ip == "" {
				continue
			}
			cidrStr := ip + "/32"
			cp, exists := cidrs[cidrStr]
			if !exists {
				cp = &struct {
					cidr api.CIDR
					*peerRules
				}{cidr: api.CIDR(cidrStr), peerRules: newPeerRules()}
				cidrs[cidrStr] = cp
				cidrOrder = append(cidrOrder, cidrStr)
			}
			cp.addFlow(fp)
			continue
		}

		// Regular endpoint
		key := peerKey(f.Destination.Labels)
		pp, exists := peers[key]
		if !exists {
			pp = &struct {
				selector api.EndpointSelector
				*peerRules
			}{
				selector:  labels.BuildPeerSelector(f.Destination.Labels, f.Destination.Namespace, policyNamespace),
				peerRules: newPeerRules(),
			}
			peers[key] = pp
			peerOrder = append(peerOrder, key)
		}
		pp.addFlow(fp)
	}

	var rules []api.EgressRule

	// Entity rules — ICMPs and ToPorts must be in separate rules per Cilium spec
	for _, entity := range entityOrder {
		er := entities[entity]
		common := api.EgressCommonRule{ToEntities: api.EntitySlice{entity}}
		if len(er.ports) > 0 {
			rules = append(rules, api.EgressRule{
				EgressCommonRule: common,
				ToPorts:          api.PortRules{{Ports: er.ports}},
			})
		}
		if len(er.icmpFields) > 0 {
			rules = append(rules, api.EgressRule{
				EgressCommonRule: common,
				ICMPs:            api.ICMPRules{{Fields: er.icmpFields}},
			})
		}
	}

	// CIDR rules
	for _, key := range cidrOrder {
		cp := cidrs[key]
		common := api.EgressCommonRule{ToCIDR: api.CIDRSlice{cp.cidr}}
		if len(cp.ports) > 0 {
			rules = append(rules, api.EgressRule{
				EgressCommonRule: common,
				ToPorts:          api.PortRules{{Ports: cp.ports}},
			})
		}
		if len(cp.icmpFields) > 0 {
			rules = append(rules, api.EgressRule{
				EgressCommonRule: common,
				ICMPs:            api.ICMPRules{{Fields: cp.icmpFields}},
			})
		}
	}

	// Endpoint selector rules
	for _, key := range peerOrder {
		pp := peers[key]
		common := api.EgressCommonRule{ToEndpoints: []api.EndpointSelector{pp.selector}}
		if len(pp.ports) > 0 {
			rules = append(rules, api.EgressRule{
				EgressCommonRule: common,
				ToPorts:          api.PortRules{{Ports: pp.ports}},
			})
		}
		if len(pp.icmpFields) > 0 {
			rules = append(rules, api.EgressRule{
				EgressCommonRule: common,
				ICMPs:            api.ICMPRules{{Fields: pp.icmpFields}},
			})
		}
	}
	return rules
}
