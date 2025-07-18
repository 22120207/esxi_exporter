package metrics

import (
	"encoding/json"
	"esxi_exporter/internal/helpers"
	"esxi_exporter/internal/models"
	"log"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	registry  *prometheus.Registry
	namespace string
	host      string
	metrics   map[string]*prometheus.GaugeVec
}

// NewMetrics initializes a new Metrics instance with Prometheus gauges
func NewMetrics() *Metrics {
	m := &Metrics{
		registry:  prometheus.NewRegistry(),
		namespace: "esxi",
		host:      "localhost",
		metrics:   make(map[string]*prometheus.GaugeVec),
	}

	// Define Prometheus gauges
	m.metrics["controller_info"] = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: m.namespace,
			Name:      "controller_info",
			Help:      "MegaRAID controller info",
		},
		[]string{"controller", "model", "serial", "fwversion"},
	)
	m.metrics["controller_status"] = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: m.namespace,
			Name:      "controller_status",
			Help:      "Controller status (1=Optimal, 0=Not Optimal)",
		},
		[]string{"controller"},
	)
	m.metrics["controller_temperature"] = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: m.namespace,
			Name:      "controller_temperature",
			Help:      "Controller temperature in Celsius",
		},
		[]string{"controller"},
	)
	m.metrics["drive_status"] = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: m.namespace,
			Name:      "drive_status",
			Help:      "Physical drive status (1=Online, 0=Other)",
		},
		[]string{"controller", "drive", "model_name", "protocol"},
	)
	m.metrics["drive_temp"] = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: m.namespace,
			Name:      "drive_temp",
			Help:      "Physical drive temperature in Celsius",
		},
		[]string{"controller", "drive"},
	)
	m.metrics["drive_smart"] = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: m.namespace,
			Name:      "drive_smart",
			Help:      "Drive SMART attributes",
		},
		[]string{"controller", "drive", "attribute"},
	)
	m.metrics["virtual_drive_status"] = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: m.namespace,
			Name:      "virtual_drive_status",
			Help:      "Virtual drive status (1=Optimal, 0=Other)",
		},
		[]string{"controller", "vd"},
	)
	m.metrics["bbu_health"] = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: m.namespace,
			Name:      "bbu_health",
			Help:      "Battery Backup Unit health (1=Healthy, 0=Unhealthy)",
		},
		[]string{"controller"},
	)
	m.metrics["smartctl_info"] = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: m.namespace,
			Name:      "smartctl_info",
			Help:      "Indicates smartctl is used for metrics collection (1=Active)",
		},
		[]string{"host"},
	)
	m.metrics["smartctl_drive"] = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: m.namespace,
			Name:      "smartctl_drive",
			Help:      "Lists drives detected via smartctl on ESXi host",
		},
		[]string{"host", "drive", "device_id", "model_name", "protocol"},
	)

	// Register all metrics
	for _, metric := range m.metrics {
		m.registry.MustRegister(metric)
	}

	return m
}

// parseSmartData converts SMART data hex string to attributes
func (m *Metrics) parseSmartData(smartDataHex string) map[string]float64 {
	attributes := make(map[string]float64)

	// Clean hex string
	re := regexp.MustCompile("[^0-9a-fA-F]")
	hexClean := re.ReplaceAllString(smartDataHex, "")

	// Convert to byte array
	byteArray := make([]int, 0, len(hexClean)/2)
	for i := 0; i < len(hexClean)-1; i += 2 {
		if val, err := strconv.ParseInt(hexClean[i:i+2], 16, 32); err == nil {
			byteArray = append(byteArray, int(val))
		}
	}

	startIndex := 0
	if len(byteArray) >= 2 && ((byteArray[0] == 0x01 && byteArray[1] == 0x00) || (byteArray[0] == 0x2f && byteArray[1] == 0x00)) {
		startIndex = 2
	}

	i := startIndex
	for i+10 < len(byteArray) {
		attrID := byteArray[i]
		if !(1 <= attrID && attrID <= 255) {
			i++
			continue
		}

		if i+10 >= len(byteArray) {
			log.Printf("Not enough bytes for attribute ID %d at index %d", attrID, i)
			break
		}

		normalizedValue := byteArray[i+3]
		rawValueBytes := byteArray[i+5 : i+11]
		rawValue := int64(0)
		for k, byteVal := range rawValueBytes {
			rawValue |= int64(byteVal) << (k * 8)
		}

		attrNameMap := map[int]string{
			0x01: "raw_read_error_rate", 0x03: "spin_up_time", 0x04: "start_stop_count", 0x05: "reallocated_sector_count",
			0x07: "seek_error_rate", 0x09: "power_on_hours", 0x0C: "power_cycle_count", 0x53: "initial_bad_block_count",
			0xB1: "wear_leveling_count", 0xB3: "used_reserved_block_count_total", 0xB4: "unused_reserved_block_count_total",
			0xB5: "program_fail_count_total", 0xB6: "erase_fail_count_total", 0xB7: "runtime_bad_block", 0xB8: "end_to_end_error",
			0xBB: "uncorrectable_error_count", 0xBE: "airflow_temperature_celsius", 0xC2: "temperature_celsius", 0xC3: "hardware_ecc_recovered",
			0xC5: "current_pending_sector_count", 0xC6: "uncorrectable_sector_count", 0xC7: "udma_crc_error_count", 0xCA: "data_address_mark_errors",
			0xEB: "por_recovery_count", 0xF1: "total_host_writes", 0xF2: "total_host_reads", 0xF3: "total_host_writes_expanded", 0xF4: "total_host_reads_expanded",
			0xF5: "remaining_rated_write_endurance", 0xF6: "cumulative_host_sectors_written", 0xF7: "host_program_page_count", 0xFB: "minimum_spares_remaining",
		}

		attrName, ok := attrNameMap[attrID]
		if !ok {
			attrName = "unknown_" + strconv.FormatInt(int64(attrID), 16)
		}

		// If it is wear_leveling_count, take raw_value and normalized_value
		if attrID == 0xB1 {
			attributes[attrName+"_raw"] = float64(rawValue)
			attributes[attrName+"_value"] = float64(normalizedValue)
		} else {
			// If it is temperature attribute, just take the first byte as value
			if attrID == 0xC2 {
				attributes[attrName] = float64(rawValueBytes[0])
			} else {
				attributes[attrName] = float64(rawValue)
			}
		}
		i += 11
	}
	return attributes
}

// discoverEsxcliDevices discovers devices using esxcli
func (m *Metrics) discoverEsxcliDevices() []map[string]string {
	detectedDevices := []map[string]string{}

	output, err := m.runCmd("esxcli storage core device list")
	if err != nil {
		log.Printf("Error discovering esxcli devices: %v", err)
		return detectedDevices
	}

	currentDeviceInfo := make(map[string]string)
	lines := strings.Split(output, "\n")

	deviceRegex, _ := regexp.Compile(`^(naa\.\S+|t10\.\S+|mpx\.\S+)$`)
	ssdRegex, _ := regexp.Compile(`Is SSD:\s*true`)

	for _, line := range lines {
		line = strings.TrimSpace(line)

		deviceIDMatch := deviceRegex.MatchString(line)
		if deviceIDMatch {
			if len(currentDeviceInfo) > 0 {
				deviceID, displayName := currentDeviceInfo["id"], currentDeviceInfo["display_name"]
				model, protocol := currentDeviceInfo["model"], currentDeviceInfo["protocol"]
				if deviceID != "" && displayName != "" {
					detectedDevices = append(detectedDevices, map[string]string{
						"id":           deviceID,
						"display_name": displayName,
						"model":        model,
						"protocol":     protocol,
					})
				} else {
					log.Printf("Skipping incomplete device info: %v", currentDeviceInfo)
				}
			}
			currentDeviceInfo = map[string]string{"id": line}
			continue
		}

		if len(currentDeviceInfo) > 0 {
			displayNameMatch := regexp.MustCompile(`Display Name:\s*(.+)`).FindStringSubmatch(line)
			if len(displayNameMatch) > 1 {
				rawDisplayName := strings.TrimSpace(displayNameMatch[1])
				cleanedDisplayName := regexp.MustCompile(`\s*\([^)]+\)$`).ReplaceAllString(rawDisplayName, "")
				currentDeviceInfo["display_name"] = cleanedDisplayName
				continue
			}

			modelMatch := regexp.MustCompile(`Model:\s*(.+)`).FindStringSubmatch(line)
			if len(modelMatch) > 1 {
				currentDeviceInfo["model"] = strings.TrimSpace(modelMatch[1])
				continue
			}

			isSSdMatch := ssdRegex.MatchString(strings.ToLower(line))
			if isSSdMatch {
				currentDeviceInfo["protocol"] = "SSD"
				continue
			}

			if strings.Contains(strings.ToLower(line), "nvme") {
				currentDeviceInfo["protocol"] = "NVMe"
			}
		}
	}

	if len(currentDeviceInfo) > 0 {
		deviceID, displayName := currentDeviceInfo["id"], currentDeviceInfo["display_name"]
		model, protocol := currentDeviceInfo["model"], currentDeviceInfo["protocol"]
		if deviceID != "" && displayName != "" {
			detectedDevices = append(detectedDevices, map[string]string{
				"id":           deviceID,
				"display_name": displayName,
				"model":        model,
				"protocol":     protocol,
			})
		} else {
			log.Printf("Skipping incomplete last device info: %v", currentDeviceInfo)
		}
	}

	return detectedDevices
}

// parseEsxcliSmart parses SMART data from esxcli/smartctl
func (m *Metrics) parseEsxcliSmart(deviceID string) map[string]float64 {
	smartAttributes := make(map[string]float64)
	cmd := "cd /opt/smartmontools && ./smartctl -a -d sat /dev/disks/" + deviceID + " | awk '/ID# ATTRIBUTE_NAME/,/Total_LBAs_Written/'"
	output, err := m.runCmd(cmd)
	if err != nil {
		log.Printf("smartctl get failed for %s: %v. This is expected for logical drives.", deviceID, err)
		return smartAttributes
	}

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ID#") || line == "" {
			continue
		}

		tokens := regexp.MustCompile(`\s+`).Split(line, -1)
		if len(tokens) < 10 {
			log.Printf("Line does not match SMART format: %s", line)
			continue
		}

		attrName, value, rawValue := tokens[1], tokens[3], tokens[len(tokens)-1]
		rawFloat, err := strconv.ParseFloat(rawValue, 64)
		if err != nil {
			log.Printf("Could not parse value/raw_value as float: '%s', '%s' for %s", value, rawValue, attrName)
			continue
		}

		attrKey := strings.ToLower(strings.ReplaceAll(attrName, "-", "_"))
		if attrKey == "wear_leveling_count" {
			valueFloat, err := strconv.ParseFloat(value, 64)
			if err == nil {
				smartAttributes["wear_leveling_count_value"] = valueFloat
				smartAttributes["wear_leveling_count_raw"] = rawFloat
			}
		} else {
			smartAttributes[attrKey] = rawFloat
		}
	}
	return smartAttributes
}

// CollectMetrics collects and sets metrics for Prometheus
func (m *Metrics) CollectMetrics() {
	// Reset metrics to ensure fresh data on each scrape
	for _, metric := range m.metrics {
		metric.Reset()
	}

	perccliData := make(map[string]interface{})
	perccliFailed := false

	stdout, err := m.runCmd("cd /opt/lsi/perccli && ./perccli /cALL show all J")
	if err != nil {
		perccliFailed = true
		log.Printf("perccli command failed: %v. Falling back to esxcli.", err)
	} else {
		if err := json.Unmarshal([]byte(stdout), &perccliData); err != nil {
			perccliFailed = true
			log.Printf("Failed to decode JSON from perccli output: %v. Falling back to esxcli.", err)
		} else {
			controllers, ok := perccliData["Controllers"].([]interface{})
			if !ok || len(controllers) == 0 {
				perccliFailed = true
				log.Println("perccli returned no controller data. Falling back to esxcli.")
			} else {
				firstController, ok := controllers[0].(map[string]interface{})
				if ok {
					commandStatus, ok := firstController["Command Status"].(map[string]interface{})
					if ok && commandStatus["Status"] == "Failure" && strings.Contains(commandStatus["Description"].(string), "No Controller found") {
						perccliFailed = true
						log.Println("perccli reported 'No Controller found'. Falling back to esxcli.")
					}
				}
			}
		}
	}

	if !perccliFailed {
		log.Println("perccli found controllers. Processing perccli data.")
		controllers := perccliData["Controllers"].([]interface{})
		for _, controller := range controllers {
			response, ok := controller.(map[string]interface{})["Response Data"].(map[string]interface{})
			if !ok {
				continue
			}
			m.handleCommonController(response)
			driverName := helpers.GetString(response["Version"].(map[string]interface{}), "Driver Name", "Unknown")
			if driverName == "megaraid_sas" || driverName == "lsi-mr3" {
				m.handleMegaraidController(response)
			}
		}
	} else {
		log.Println("perccli not used. Discovering and processing devices via esxcli.")
		m.metrics["smartctl_info"].With(prometheus.Labels{"host": m.host}).Set(1)
		esxcliDevices := m.discoverEsxcliDevices()
		for _, device := range esxcliDevices {
			deviceID, displayName := device["id"], device["display_name"]
			model, protocol := device["model"], device["protocol"]
			if model == "" {
				model = "Unknown"
			}
			if protocol == "" {
				protocol = "Unknown"
			}
			log.Printf("Processing esxcli device: %s (%s)", displayName, deviceID)
			m.metrics["smartctl_drive"].With(prometheus.Labels{
				"host":       m.host,
				"drive":      displayName,
				"device_id":  deviceID,
				"model_name": model,
				"protocol":   protocol,
			}).Set(1)
			m.metrics["drive_status"].With(prometheus.Labels{
				"controller": "esxcli",
				"drive":      displayName,
				"model_name": model,
				"protocol":   protocol,
			}).Set(1) // Assuming status=1 for detected drives
			smartAttrs := m.parseEsxcliSmart(deviceID)
			if len(smartAttrs) > 0 {
				for attr, value := range smartAttrs {
					m.metrics["drive_smart"].With(prometheus.Labels{
						"controller": "esxcli",
						"drive":      displayName,
						"attribute":  attr,
					}).Set(value)
				}
			} else {
				log.Printf("No SMART data collected via esxcli for device: %s (%s)", displayName, deviceID)
			}
		}
	}
}

// handleCommonController processes common controller metrics
func (m *Metrics) handleCommonController(response map[string]interface{}) {
	basics, ok := response["Basics"].(map[string]interface{})
	if !ok {
		return
	}
	controllerIndex := helpers.GetString(basics, "Controller", "Unknown")
	model := helpers.GetString(basics, "Model", "Unknown")
	serial := helpers.GetString(basics, "Serial Number", "Unknown")
	fwversion := helpers.GetString(response["Version"].(map[string]interface{}), "Firmware Version", "Unknown")

	m.metrics["controller_info"].With(prometheus.Labels{
		"controller": controllerIndex,
		"model":      model,
		"serial":     serial,
		"fwversion":  fwversion,
	}).Set(1)

	statusMap, ok := response["Status"].(map[string]interface{})
	if ok && statusMap["Controller Status"] == "Optimal" {
		m.metrics["controller_status"].With(prometheus.Labels{"controller": controllerIndex}).Set(1)
	} else {
		m.metrics["controller_status"].With(prometheus.Labels{"controller": controllerIndex}).Set(0)
	}

	hwCfg, ok := response["HwCfg"].(map[string]interface{})
	if ok {
		for _, key := range []string{"ROC temperature(Degree Celcius)", "ROC temperature(Degree Celsius)"} {
			if temp, ok := hwCfg[key]; ok {
				if tempFloat, err := strconv.ParseFloat(temp.(string), 64); err == nil {
					m.metrics["controller_temperature"].With(prometheus.Labels{"controller": controllerIndex}).Set(tempFloat)
				}
				break
			}
		}
	}
}

// handleMegaraidController processes MegaRAID-specific controller data
func (m *Metrics) handleMegaraidController(response map[string]interface{}) {
	controllerIndex := helpers.GetString(response["Basics"].(map[string]interface{}), "Controller", "Unknown")

	pdList, ok := response["PD LIST"].([]interface{})
	if ok {
		for _, drive := range pdList {
			driveMap, ok := drive.(map[string]interface{})
			if !ok {
				continue
			}
			eidSlt := helpers.GetString(driveMap, "EID:Slt", "0:0")
			parts := strings.SplitN(eidSlt, ":", 2)
			enclosure, slot := parts[0], parts[1]
			drivePath := "/c" + controllerIndex + "/e" + enclosure + "/s" + slot
			smartData := m.getPerccliSmart(drivePath)
			smartAttributes := m.parseSmartData(smartData)
			m.createMetricsOfPhysicalDrive(driveMap, controllerIndex, smartAttributes)
		}
	}

	vdList, ok := response["VD LIST"].([]interface{})
	if ok {
		for _, vd := range vdList {
			vdMap, ok := vd.(map[string]interface{})
			if !ok {
				continue
			}
			vdPosition := helpers.GetString(vdMap, "DG/VD", "0/0")
			parts := strings.SplitN(vdPosition, "/", 2)
			driveGroup, volumeGroup := parts[0], parts[1]
			vdID := "DG" + driveGroup + "/VD" + volumeGroup
			var status float64
			if helpers.GetString(vdMap, "State", "Unknown") == "Optl" {
				status = 1
			}
			m.metrics["virtual_drive_status"].With(prometheus.Labels{
				"controller": controllerIndex,
				"vd":         vdID,
			}).Set(status)
		}
	}

	bbuStatus, ok := response["Status"].(map[string]interface{})["BBU Status"]
	if ok && bbuStatus != "NA" {
		bbuHealth := 0.0
		if bbuStatusFloat, err := strconv.ParseFloat(bbuStatus.(string), 64); err == nil {
			if bbuStatusFloat == 0 || bbuStatusFloat == 8 || bbuStatusFloat == 4096 {
				bbuHealth = 1
			}
		}
		m.metrics["bbu_health"].With(prometheus.Labels{"controller": controllerIndex}).Set(bbuHealth)
	}
}

// createMetricsOfPhysicalDrive sets metrics for a physical drive
func (m *Metrics) createMetricsOfPhysicalDrive(physicalDrive map[string]interface{}, controllerIndex string, smartAttributes map[string]float64) {
	eidSlt := helpers.GetString(physicalDrive, "EID:Slt", "0:0")
	parts := strings.SplitN(eidSlt, ":", 2)
	enclosure, slot := parts[0], parts[1]
	driveIdentifier := "Drive /c" + controllerIndex + "/e" + enclosure + "/s" + slot
	state := helpers.GetString(physicalDrive, "State", "Unknown")
	var status float64
	if state == "Onln" {
		status = 1
	}
	modelName := helpers.GetString(physicalDrive, "Model", "Unknown")
	protocol := helpers.GetString(physicalDrive, "Intf", "Unknown")

	m.metrics["drive_status"].With(prometheus.Labels{
		"controller": controllerIndex,
		"drive":      driveIdentifier,
		"model_name": modelName,
		"protocol":   protocol,
	}).Set(status)

	if temp, ok := physicalDrive["Temp"]; ok {
		tempStr := strings.ReplaceAll(temp.(string), "C", "")
		if tempFloat, err := strconv.ParseFloat(tempStr, 64); err == nil {
			m.metrics["drive_temp"].With(prometheus.Labels{
				"controller": controllerIndex,
				"drive":      driveIdentifier,
			}).Set(tempFloat)
		} else {
			log.Printf("Could not parse temperature for %s: %v", driveIdentifier, temp)
		}
	}

	for attr, value := range smartAttributes {
		m.metrics["drive_smart"].With(prometheus.Labels{
			"controller": controllerIndex,
			"drive":      driveIdentifier,
			"attribute":  attr,
		}).Set(value)
	}
}

// getPerccliSmart retrieves SMART data for a drive
func (m *Metrics) getPerccliSmart(drivePath string) string {
	// cd /opt/lsi/perccli && ./perccli /c0/e32/s2 show smart
	cmd := "cd /opt/lsi/perccli && ./perccli " + drivePath + " show smart"
	output, err := m.runCmd(cmd)
	if err != nil {
		log.Printf("Error getting SMART data for %s: %v", drivePath, err)
		return ""
	}

	re := regexp.MustCompile(`Smart Data Info .*? = \n([0-9a-fA-F \n]+)`)
	matches := re.FindStringSubmatch(output)
	if len(matches) < 2 {
		log.Printf("No SMART data found for %s", drivePath)
		return ""
	}
	return strings.ReplaceAll(matches[1], "\n", "")
}

// runCmd executes a shell command with a timeout
func (m *Metrics) runCmd(command string) (string, error) {
	log.Printf("Executing command: %s", command)
	cmd := exec.Command("bash", "-c", command)
	var output strings.Builder
	cmd.Stdout = &output
	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		log.Printf("Error starting command: %v", err)
		return "", err
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-time.After(30 * time.Second):
		cmd.Process.Kill()
		return "", &models.TimeoutError{Stderr: stderr.String(), Message: "Command timed out after 30 seconds"}
	case err := <-done:
		if err != nil {
			log.Printf("Command failed: %v", stderr.String())
			return "", err
		}
		return output.String(), nil
	}
}
