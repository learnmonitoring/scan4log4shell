package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/hupe1980/log4shellscan/internal"
	"github.com/spf13/cobra"
	"golang.org/x/net/html"
)

const (
	noCatcher = "none"
)

type remoteOptions struct {
	allChecks          bool
	basicAuth          string
	caddr              string
	requestTypes       []string
	proxy              string
	catcherType        string
	resource           string
	noUserAgentFuzzing bool
	authFuzzing        bool
	formFuzzing        bool
	noRedirect         bool
	noWaitTimeout      bool
	wafBypass          bool
	timeout            time.Duration
	wait               time.Duration
	headersFile        string
	headers            []string
	headerValues       map[string]string
	fieldsFile         string
	fields             []string
	fieldValues        map[string]string
	paramsFile         string
	params             []string
	paramValues        map[string]string
	payloadsFile       string
	payloads           []string
	maxThreads         int
	checkCVE2021_45046 bool
}

func newRemoteCmd(noColor *bool, output *string, verbose *bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:                   "remote",
		Short:                 "Send specially crafted requests and catch callbacks of systems that are impacted by log4j log4shell vulnerability",
		SilenceUsage:          true,
		DisableFlagsInUseLine: true,
	}

	cmd.AddCommand(
		newRemoteCIDRCmd(noColor, output, verbose),
		newRemoteURLCmd(noColor, output, verbose),
	)

	return cmd
}

func addRemoteFlags(cmd *cobra.Command, opts *remoteOptions) {
	cmd.Flags().BoolVarP(&opts.allChecks, "all", "a", false, "shortcut to run all checks")
	cmd.Flags().StringVarP(&opts.headersFile, "headers-file", "", "", "use custom headers from file")
	cmd.Flags().StringVarP(&opts.fieldsFile, "fields-file", "", "", "use custom field from file")
	cmd.Flags().StringVarP(&opts.paramsFile, "params-file", "", "", "use custom query params from file")
	cmd.Flags().StringVarP(&opts.payloadsFile, "payloads-file", "", "", "use custom payloads from file")
	cmd.Flags().StringVarP(&opts.basicAuth, "basic-auth", "", "", "basic auth credentials (eg. user:pass)")
	cmd.Flags().StringVarP(&opts.caddr, "caddr", "", "", "address to catch the callbacks (eg. ip:port)")
	cmd.Flags().StringSliceVarP(&opts.requestTypes, "type", "t", []string{"get"}, "get, post or json")
	cmd.Flags().StringVarP(&opts.proxy, "proxy", "", "", "proxy url")
	cmd.Flags().StringVarP(&opts.resource, "resource", "r", "l4s", "resource in payload")
	cmd.Flags().StringVarP(&opts.catcherType, "catcher-type", "", "dns", "type of callback catcher (dns | ldap | tcp | none)")
	cmd.Flags().BoolVarP(&opts.noUserAgentFuzzing, "no-user-agent-fuzzing", "", false, "exclude user-agent header from fuzzing")
	cmd.Flags().BoolVarP(&opts.authFuzzing, "auth-fuzzing", "", false, "add auth fuzzing")
	cmd.Flags().BoolVarP(&opts.formFuzzing, "form-fuzzing", "", false, "add form submits to fuzzing")
	cmd.Flags().BoolVarP(&opts.noRedirect, "no-redirect", "", false, "do not follow redirects")
	cmd.Flags().BoolVarP(&opts.noWaitTimeout, "no-wait-timeout", "", false, "wait forever for callbacks")
	cmd.Flags().BoolVarP(&opts.wafBypass, "waf-bypass", "", false, "extend scans with WAF bypass payload ")
	cmd.Flags().DurationVarP(&opts.wait, "wait", "w", 5*time.Second, "wait time to catch callbacks")
	cmd.Flags().DurationVarP(&opts.timeout, "timeout", "", 3*time.Second, "time limit for requests")
	cmd.Flags().IntVarP(&opts.maxThreads, "max-threads", "", 150, "max number of concurrent threads")
	cmd.Flags().BoolVarP(&opts.checkCVE2021_45046, "check-cve-2021-45046", "", false, "check for CVE-2021-45046")
	cmd.Flags().StringSliceVarP(&opts.headers, "header", "", nil, "header to use")
	cmd.Flags().StringSliceVarP(&opts.fields, "field", "", nil, "field to use")
	cmd.Flags().StringSliceVarP(&opts.params, "param", "", nil, "query param to use")
	cmd.Flags().StringSliceVarP(&opts.payloads, "payload", "", nil, "payload to use")
	cmd.Flags().StringToStringVarP(&opts.headerValues, "set-header", "", nil, "set fix header value (key=value)")
	cmd.Flags().StringToStringVarP(&opts.fieldValues, "set-field", "", nil, "set fix field value (key=value)")
	cmd.Flags().StringToStringVarP(&opts.paramValues, "set-param", "", nil, "set fix query param value (key=value)")
}

func allChecksShortcut(opts *remoteOptions) {
	if opts.allChecks {
		opts.authFuzzing = true
		opts.formFuzzing = true
		opts.wafBypass = true
		opts.checkCVE2021_45046 = true
		opts.requestTypes = []string{"get", "post", "json"}
	}
}

var unauthorizedHandler = func(verbose bool) internal.StatusCodeHandlerFunc {
	return func(ctx context.Context, client *http.Client, resp *http.Response, req *http.Request, payload string, opts *internal.RemoteOptions) {
		auth := resp.Header.Get("WWW-Authenticate")

		if strings.HasPrefix(auth, "Basic") {
			if verbose {
				printInfo("Checking %s for %s with basic auth", payload, req.URL.String())
			}

			req.SetBasicAuth(payload, payload)

			resp, err := client.Do(req)
			if err != nil {
				// ignore
				return
			}

			resp.Body.Close()
		} else {
			if verbose {
				printInfo("Checking %s for %s with bearer", payload, req.URL.String())
			}

			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", payload))

			resp, err := client.Do(req)
			if err != nil {
				// ignore
				return
			}

			resp.Body.Close()
		}
	}
}

var submitFormHanlder = func(verbose bool) internal.StatusCodeHandlerFunc {
	return func(ctx context.Context, client *http.Client, resp *http.Response, req *http.Request, payload string, opts *internal.RemoteOptions) {
		root, err := html.Parse(resp.Body)
		if err != nil {
			//ignore
			return
		}

		forms := internal.ParseForms(root)
		if len(forms) == 0 {
			if verbose {
				printInfo("No forms found in response from %s/%s", req.URL.Host, req.URL.Path)
			}
			return
		}

		var wg sync.WaitGroup

		for _, form := range forms {
			wg.Add(1)

			go func(form internal.HTMLForm) {
				defer wg.Done()

				actionURL, err := url.Parse(form.Action)
				if err != nil {
					return
				}

				for k := range form.Values {
					form.Values.Set(k, payload)
				}

				actionURL = resp.Request.URL.ResolveReference(actionURL)

				if actionURL.Hostname() != req.URL.Host {
					if verbose {
						printInfo("Hostname %s out of scope", actionURL.Hostname())
					}

					return
				}

				submitReq, err := http.NewRequestWithContext(ctx, form.Method, actionURL.String(), strings.NewReader(form.Values.Encode()))
				if err != nil {
					return
				}

				submitReq.Header = req.Header

				if verbose {
					printInfo("Checking %s for %s", payload, actionURL)
				}

				resp, err := client.Do(req)
				//resp, err = client.PostForm(actionURL.String(), form.Values)
				if err != nil {
					// ignore
					return
				}

				resp.Body.Close()
			}(form)
		}

		wg.Wait()
	}
}
