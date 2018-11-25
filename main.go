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
	//"time"
	//"fmt"
	"github.com/miekg/dns"
	"github.com/asaskevich/govalidator"
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

var DNSRecords []DNSRecord
var MXRecords []MXRecord

func getRecordsForDomain(domain string) ([]string, []string, []MXRecord) {
	var ipv4_addresses []string
	var ipv6_addresses []string
	var mx_addresses []MXRecord

	// Perform memory lookup
	DOMAIN_EXISTS := false
	for i := 0; i < len(DNSRecords); i++ {
		knownDomain := DNSRecords[i]
		if domain == knownDomain.Name {
			DOMAIN_EXISTS = true
			if knownDomain.Type == "A" {
				ipv4_addresses = append(ipv4_addresses, knownDomain.Value)
			}
			if knownDomain.Type == "AAAA" {
				ipv6_addresses = append(ipv6_addresses, knownDomain.Value)
			}
		}
	}

	MX_EXISTS := false
	for i := 0; i < len(mx_addresses); i++ {
		knownDomain := MXRecords[i]
		if domain == knownDomain.Name {
			MX_EXISTS = true
			mx_addresses = append(mx_addresses, MXRecords[i])
		}
	}

	if ! DOMAIN_EXISTS {
		// Address unknown, perform lookup
		ips, _ := net.LookupIP(domain)
		for i := 0; i < len(ips); i++ {
			if govalidator.IsIPv4(ips[i].String()) {
				record := DNSRecord{
					Name: domain,
					Type: "A",
					Value: ips[i].String(),
				}
				DNSRecords = append(DNSRecords, record)
				ipv4_addresses = append(ipv4_addresses, ips[i].String())
			}
			if govalidator.IsIPv6(ips[i].String()) {
				record := DNSRecord{
					Name: domain,
					Type: "AAAA",
					Value: ips[i].String(),
				}
				DNSRecords = append(DNSRecords, record)
				ipv6_addresses = append(ipv6_addresses, ips[i].String())
			}
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
			MXRecords = append(MXRecords, record)
		}
	}

	return ipv4_addresses, ipv6_addresses, mx_addresses
}

type handler struct{}
func (this *handler) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
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
	// This gives quick DNS stats
	//go func() {
	//	for {
			//log.Println(DNSRecords)
			//log.Println(MXRecords)
			//time.Sleep(time.Duration(10 * time.Second))
	//	}
	//}()
	srv := &dns.Server{Addr: ":" + strconv.Itoa(53), Net: "udp"}
	srv.Handler = &handler{}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Failed to set udp listener %s\n", err.Error())
	}
}