package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:  "gohfinder",
		Usage: "Find hostnames from ASN or CIDR - Robtex x BGP.HE",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "cidr",
				Aliases: []string{"c"},
				Usage:   "CIDR(s) (Single or multiple separated by commas)",
			},
			&cli.StringFlag{
				Name:    "asn",
				Aliases: []string{"a"},
				Usage:   "ASN(s) (Single or multiple separated by commas)",
			},
			&cli.BoolFlag{
				Name:  "hosts",
				Usage: "Generate /etc/hosts like file",
			},
			&cli.BoolFlag{
				Name:  "fqdn",
				Usage: "Only display found FQDN",
			},
			&cli.StringFlag{
				Name:  "filter",
				Usage: "Filter FQDN against regex",
			},
		},
		Action: func(c *cli.Context) error {
			if c.String("cidr") != "" {
				cidrs := strings.Split(c.String("cidr"), ",")
				results := make(map[string]map[string]struct{})
				for _, cidr := range cidrs {
					res := searchCIDR(cidr)
					for k, v := range res {
						if _, exists := results[k]; !exists {
							results[k] = make(map[string]struct{})
						}
						for ip := range v {
							results[k][ip] = struct{}{}
						}
					}
				}
				printResults(results, c.Bool("hosts"), c.Bool("fqdn"), c.String("filter"))
			} else if c.String("asn") != "" {
				asns := strings.Split(c.String("asn"), ",")
				results := make(map[string]map[string]struct{})
				for _, asn := range asns {
					ranges := searchASN(asn)
					for _, rangeCIDR := range ranges {
						res := searchCIDR(rangeCIDR)
						for k, v := range res {
							if _, exists := results[k]; !exists {
								results[k] = make(map[string]struct{})
							}
							for ip := range v {
								results[k][ip] = struct{}{}
							}
						}
					}
				}
				printResults(results, c.Bool("hosts"), c.Bool("fqdn"), c.String("filter"))
			} else {
				return fmt.Errorf("Invalid parameters. Please provide either -c or -a")
			}
			return nil
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

// Set a custom User-Agent to avoid being blocked by websites.
const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/110.0.0.0 Safari/537.36"

func searchASN(asns string) []string {
	bgpURL := "https://bgp.he.net/"
	asnList := strings.Split(asns, ",")
	var ranges []string

	for _, asn := range asnList {
		req, err := http.NewRequest("GET", bgpURL+asn, nil)
		if err != nil {
			log.Fatalf("Failed to create HTTP request: %v", err)
			return nil
		}

		// Set a custom User-Agent
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/110.0.0.0 Safari/537.36")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			//			log.Printf("Failed to fetch BGP.HE ASN page: %v", err)
			continue
		}
		defer resp.Body.Close()

		// Check if the request was successful
		if resp.StatusCode != http.StatusOK {
			//			log.Printf("Failed to fetch BGP.HE ASN page: status code %d", resp.StatusCode)
			continue
		}

		// Parse the HTML
		doc, err := goquery.NewDocumentFromReader(resp.Body)
		if err != nil {
			//			log.Printf("Error parsing ASN HTML: %v", err)
			continue
		}

		// Extract ASN ranges from the specified table
		doc.Find("#table_prefixes4 tbody tr").Each(func(i int, s *goquery.Selection) {
			// Extract prefix (the first <td> which contains the <a> tag)
			prefix := s.Find("td").First().Find("a").Text()

			// Clean up the string
			prefix = strings.TrimSpace(prefix)

			// Add the prefix to the ranges if it's not empty
			if prefix != "" {
				ranges = append(ranges, prefix)
			}
		})

		for _, rangeEntry := range ranges {
			fmt.Println(rangeEntry) // Print each range on a new line
		}
	}

	return ranges
}

func searchCIDR(cidr string) map[string]map[string]struct{} {
	robtexURL := "https://www.robtex.com/cidr/"
	uri := strings.Replace(cidr, "/", "-", -1)

	// Create a new HTTP request
	req, err := http.NewRequest("GET", robtexURL+uri, nil)
	if err != nil {
		log.Fatalf("Failed to create HTTP request: %v", err)
		return nil
	}

	// Set the User-Agent header to avoid blocking
	req.Header.Set("User-Agent", userAgent)

	// Send the HTTP request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		//		log.Printf("Failed to fetch Robtex CIDR page: %v", err)
		return nil
	}
	defer resp.Body.Close()

	// Check if the request was successful
	if resp.StatusCode != http.StatusOK {
		//		log.Printf("Failed to fetch Robtex CIDR page: status code %d", resp.StatusCode)
		return nil
	}

	// Parse the HTML using goquery
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		//		log.Fatalf("Error parsing CIDR HTML: %v", err)
		return nil
	}

	// Collect hostnames and IPs
	hostnames := make(map[string]map[string]struct{})
	doc.Find("a[href^='https://www.robtex.com/dns-lookup/']").Each(func(i int, s *goquery.Selection) {
		h := strings.Replace(s.AttrOr("href", ""), "https://www.robtex.com/dns-lookup/", "", 1)
		ip := s.Parent().Parent().Find("a[href^='https://www.robtex.com/ip-lookup/']").AttrOr("href", "")
		ip = strings.Replace(ip, "https://www.robtex.com/ip-lookup/", "", 1)

		if _, exists := hostnames[h]; !exists {
			hostnames[h] = make(map[string]struct{})
		}
		hostnames[h][ip] = struct{}{}
	})

	return hostnames
}

func printResults(results map[string]map[string]struct{}, hosts bool, fqdn bool, filter string) {
	var re *regexp.Regexp
	var err error
	if filter != "" {
		// Compile the regex pattern
		re, err = regexp.Compile(filter)
		if err != nil {
			log.Fatalf("Invalid regex pattern: %v", err)
		}
	}

	if hosts {
		hostsResult := make(map[string]map[string]struct{})
		for hostname, ips := range results {
			if filter == "" || re.MatchString(hostname) {
				for ip := range ips {
					if _, exists := hostsResult[ip]; !exists {
						hostsResult[ip] = make(map[string]struct{})
					}
					hostsResult[ip][hostname] = struct{}{}
				}
			}
		}

		for ip, hostnames := range hostsResult {
			var hostnameList []string
			for h := range hostnames {
				hostnameList = append(hostnameList, h)
			}
			fmt.Printf("%s %s\n", ip, strings.Join(hostnameList, " "))
		}
	} else {
		for hostname, ips := range results {
			if filter == "" || re.MatchString(hostname) {
				var ipList []string
				for ip := range ips {
					ipList = append(ipList, ip)
				}
				if fqdn {
					fmt.Println(hostname)
				} else {
					fmt.Printf("%s: %s\n", hostname, strings.Join(ipList, " "))
				}
			}
		}
	}
}
