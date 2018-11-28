// This was originally taken from: https://jameshfisher.com/2017/08/04/golang-dns-server.html

// The goal of this was to write a DNS caching server, where it will recursively resolve a record
// if it's not already been loaded into memory. 
// Currently only works with A/AAAA/MX record types
// To-do: TXT/Remaining record types


package main

import (
	"net"
	"strconv"
	"log"
	"fmt"
	"github.com/miekg/dns"
	"github.com/asaskevich/govalidator"
	"github.com/derekparker/trie"
	"net/http"
	_ "net/http/pprof"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"flag"
)

type MXRecord struct {
	Name string
	Priority uint16
	Value string
}

type DNSRecord struct {
	Name string
	Type string
	Value string
}

var (
	RecordsGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dns_records",
			Help: "Statistics on dns records collected",
		},
		[]string{
			"type",
		},
	)
)

var (
        exporter_addr = flag.String("exporter.addr", ":9100", "Address for the Prometheus Exporter to bind to")
        DEBUG = flag.Bool("debug", false, "Debug mode")
)

var DNSRecords = trie.New()
var MXRecords = trie.New()

func debugLog(msg string) {
	//if DEBUG {
	log.Println(msg)
	//}
}

func getRecordsForDomain(domain string) ([]string, []string, []MXRecord) {
	var ipv4_addresses []string
	var ipv6_addresses []string
	var mx_addresses []MXRecord

	// Perform memory lookup
	DOMAIN_EXISTS := false
	d, ok := DNSRecords.Find(domain)
	if ! ok {
		debugLog("Domain not in memory (trie)")
	} else {
		DOMAIN_EXISTS = true
		addresses := d.Meta().([]DNSRecord)
		for i := 0; i < len(addresses); i++ {
			if addresses[i].Type == "A" {
				ipv4_addresses = append(ipv4_addresses, addresses[i].Value)
			}
			if addresses[i].Type == "AAAA" {
				ipv6_addresses = append(ipv6_addresses, addresses[i].Value)
			}
		}
	}

	MX_EXISTS := false
	m, ok := MXRecords.Find(domain)
	if ! ok {
		debugLog("MX not in memory (trie)")
	} else {
		MX_EXISTS = true
		addresses := m.Meta().([]MXRecord)
		for i := 0; i <len(addresses); i++ {
			mx_addresses = append(mx_addresses, addresses[i])
			lookup_domain := addresses[i].Value
			go func() {
				// Perform automatic MX crawl/lookup in background
				getRecordsForDomain(lookup_domain)
			}()
		}
	}

	if ! DOMAIN_EXISTS {
		// Address unknown, perform lookup
		debugLog("Domain unknown, performing lookup")
		var dns_records = []DNSRecord{}
		ips, _ := net.LookupIP(domain)
		for i := 0; i < len(ips); i++ {
			if govalidator.IsIPv4(ips[i].String()) {
				record := DNSRecord{
					Name: domain,
					Type: "A",
					Value: ips[i].String(),
				}
				dns_records = append(dns_records, record)
				ipv4_addresses = append(ipv4_addresses, ips[i].String())
				RecordsGauge.With(prometheus.Labels{
					"type": "A",
				}).Inc()
			}
			if govalidator.IsIPv6(ips[i].String()) {
				record := DNSRecord{
					Name: domain,
					Type: "AAAA",
					Value: ips[i].String(),
				}
				dns_records = append(dns_records, record)
				ipv6_addresses = append(ipv6_addresses, ips[i].String())
				RecordsGauge.With(prometheus.Labels{
					"type": "AAAA",
				}).Inc()
			}
		}

		// Create records with all returned addresses
		d, ok := DNSRecords.Find(domain)
		if ! ok {
			DNSRecords.Add(domain, dns_records)
			debugLog(fmt.Sprintf("Domain %s added to memory (trie)", domain))
		} else {
			fmt.Println(d)
			debugLog(fmt.Sprintf("Domain not created, but in ! DOMAIN_EXISTS block - this seems bad:", domain))
		}
	}

	if ! MX_EXISTS {
		mxs, _ := net.LookupMX(domain)
		for i := 0; i < len(mxs); i++ {
			record := MXRecord{
				Name: domain,
				Priority: mxs[i].Pref,
				Value: mxs[i].Host,
			}
			mx_addresses = append(mx_addresses, record)
		}
		m, ok := MXRecords.Find(domain)
		if ! ok {
			MXRecords.Add(domain, mx_addresses)
			RecordsGauge.With(prometheus.Labels{
				"type": "MX",
			}).Inc()
			debugLog(fmt.Sprintf("Domain %s added to memory (trie)", domain))
		} else {
			fmt.Println(m)
			debugLog(fmt.Sprintf("Domain MX not created, but in !MX_EXISTS block - this seems bad:", domain))
		}
	}

	return ipv4_addresses, ipv6_addresses, mx_addresses
}

type handler struct{}
func (this *handler) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	//log.Println(DNSRecords.Find("google.com"))
	msg := dns.Msg{}
	msg.SetReply(r)
	domain := msg.Question[0].Name
	msg.Authoritative = true
	ipv4_addresses, ipv6_addresses, mx_addresses := getRecordsForDomain(domain)
	switch r.Question[0].Qtype {
	case dns.TypeA:
		for i := 0; i < len(ipv4_addresses); i++ {
			msg.Answer = append(msg.Answer, &dns.A{
				Hdr: dns.RR_Header{ Name: domain, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60 },
				A: net.ParseIP(ipv4_addresses[i]),
			})
		}
	case dns.TypeAAAA:
		for i := 0; i < len(ipv6_addresses); i++ {
			msg.Answer = append(msg.Answer, &dns.AAAA{
				Hdr: dns.RR_Header{ Name: domain, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 60 },
				AAAA: net.ParseIP(ipv6_addresses[i]),
			})
		}
	case dns.TypeMX:
		for i := 0; i < len(mx_addresses); i++ {
			msg.Answer = append(msg.Answer, &dns.MX{
				Hdr: dns.RR_Header{ Name: domain, Rrtype: dns.TypeMX, Class: dns.ClassINET, Ttl: 60 },
				Preference: mx_addresses[i].Priority,
				Mx: mx_addresses[i].Value, 
			})
		}
	}
	w.WriteMsg(&msg)
}

func main() {
	log.Println("Starting Gocache...")
	flag.Parse()

	// pprof
	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()

	go func() {
		// Start prometheus exporter
		prometheus.MustRegister(RecordsGauge)
		http.Handle("/metrics", promhttp.Handler())
		log.Fatal(http.ListenAndServe(*exporter_addr, nil))
	}()

	srv := &dns.Server{Addr: ":" + strconv.Itoa(53), Net: "udp"}
	srv.Handler = &handler{}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Failed to set udp listener %s\n", err.Error())
	}
}
