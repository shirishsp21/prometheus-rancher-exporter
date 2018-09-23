package main

import (
	"encoding/json"
	"net/http"
	"crypto/tls"
	"strconv"
	"strings"
	"time"

	"github.com/infinityworks/prometheus-rancher-exporter/measure"
	"github.com/prometheus/client_golang/prometheus"
)

// Data is used to store data from all the relevant endpoints in the API
type Data struct {
	Data []struct {
		HealthState string `json:"healthState"`
		Name        string `json:"name"`
		State       string `json:"state"`
		System      bool   `json:"system"`
		Scale       int    `json:"scale"`
		HostName    string `json:"hostname"`
		ID          string `json:"id"`
		StackID     string `json:"stackId"`
		EnvID       string `json:"environmentId"`
		BaseType    string `json:"basetype"`
		Type        string `json:"type"`
		AgentState  string `json:"agentState"`
	} `json:"data"`
}

// processMetrics - Collects the data from the API, returns data object
func (e *Exporter) processMetrics(data *Data, endpoint string, hideSys bool, ch chan<- prometheus.Metric) error {

	// Metrics - range through the data object
	for _, x := range data.Data {

		// If system services have been ignored, the loop simply skips them
		if hideSys == true && x.System == true {
			continue
		}

		// Checks the metric is of the expected type
		dataType := x.BaseType
		if dataType == "" {
			dataType = x.Type
		}
		if checkMetric(endpoint, dataType) == false {
			continue
		}

		log.Debug("Processing metrics for %s", endpoint)

		if endpoint == "hosts" {
			var s = x.HostName
			if x.Name != "" {
				s = x.Name
			}
			if err := e.setHostMetrics(s, x.State, x.AgentState); err != nil {
				log.Errorf("Error processing host metrics: %s", err)
				log.Errorf("Attempt Failed to set %s, %s, [agent] %s ", x.HostName, x.State, x.AgentState)

				continue
			}

		} else if endpoint == "stacks" {

			// Used to create a map of stackID and stackName
			// Later used as a dimension in service metrics
			stackRef = storeStackRef(x.ID, x.Name)

			if err := e.setStackMetrics(x.Name, x.State, x.HealthState, strconv.FormatBool(x.System)); err != nil {
				log.Errorf("Error processing stack metrics: %s", err)
				log.Errorf("Attempt Failed to set %s, %s, %s, %t", x.Name, x.State, x.HealthState, x.System)
				continue
			}

		} else if endpoint == "services" {

			// Retrieves the stack Name from the previous values stored.
			var stackName = retrieveStackRef(x.StackID)

			if stackName == "unknown" {
				log.Warnf("Failed to obtain stack_name for %s from the API", x.Name)
			}

			if err := e.setServiceMetrics(x.Name, stackName, x.State, x.HealthState, x.Scale); err != nil {
				log.Errorf("Error processing service metrics: %s", err)
				log.Errorf("Attempt Failed to set %s, %s, %s, %s, %d", x.Name, stackName, x.State, x.HealthState, x.Scale)
				continue
			}

			e.setServiceMetrics(x.Name, stackName, x.State, x.HealthState, x.Scale)
		}

	}

	return nil
}

// gatherData - Collects the data from thw API, invokes functions to transform that data into metrics
func (e *Exporter) gatherData(rancherURL string, accessKey string, secretKey string, endpoint string, ch chan<- prometheus.Metric) (*Data, error) {

	// Return the correct URL path
	url := setEndpoint(rancherURL, endpoint)

	// Create new data slice from Struct
	var data = new(Data)

	// Scrape EndPoint for JSON Data
	err := getJSON(url, accessKey, secretKey, &data)
	if err != nil {
		log.Error("Error getting JSON from endpoint ", endpoint)
		return nil, err
	}
	log.Debugf("JSON Fetched for: "+endpoint+": ", data)

	return data, err
}

// getJSON return json from server, return the formatted JSON
func getJSON(url string, accessKey string, secretKey string, target interface{}) error {

	start := time.Now()

	// Counter for internal exporter metrics
	measure.FunctionCountTotal.With(prometheus.Labels{"pkg": "main", "fnc": "getJSON"}).Inc()

	log.Info("Scraping: ", url)

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	client := &http.Client{Transport: tr}
	req, err := http.NewRequest("GET", url, nil)

	if err != nil {
		log.Error("Error Collecting JSON from API: ", err)
	}

	req.SetBasicAuth(accessKey, secretKey)
	resp, err := client.Do(req)

	if err != nil {
		log.Error("Error Collecting JSON from API: ", err)
	}

	if ! strings.Contains(resp.Status, "200") {
		log.Error("Error returned from API: ",resp.Status) 	
	}	
	
	respFormatted := json.NewDecoder(resp.Body).Decode(target)

	// Timings recorded as part of internal metrics
	elapsed := float64((time.Since(start)) / time.Microsecond)
	measure.FunctionDurations.WithLabelValues("main", "getJSON").Observe(elapsed)

	// Close the response body, the underlying Transport should then close the connection.
	resp.Body.Close()

	// return formatted JSON
	return respFormatted
}

// setEndpoint - Determines the correct URL endpoint to use, gives us backwards compatibility
func setEndpoint(rancherURL string, component string) string {

	var endpoint string

	endpoint = (rancherURL + "/" + component + "/")
    endpoint = strings.Replace(endpoint, "v1", "v2-beta", 1)

	return endpoint
}

// storeStackRef stores the stackID and stack name for use as a label elsewhere
func storeStackRef(stackID string, stackName string) map[string]string {

	stackRef[stackID] = stackName

	return stackRef
}

// retrieveStackRef returns the stack name, when sending the stackID
func retrieveStackRef(stackID string) string {

	for key, value := range stackRef {
		if stackID == "" {
			return "unknown"
		} else if stackID == key {
			log.Debugf("StackRef - Key is %s, Value is %s StackID is %s", key, value, stackID)
			return value
		}
	}
	// returns unknown if no match was found
	return "unknown"
}
