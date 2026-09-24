package model

import (
	"maps"
	"slices"
	"strings"
)

const nodeFS = `fstype!~"tmpfs|overlay|squashfs|devtmpfs|ramfs|nsfs|autofs|proc|sysfs|cgroup2?|fuse.lxcfs"`
const nodeNet = `device!~"lo|veth.*|docker.*|br-.*|virbr.*|cali.*|flannel.*|cni.*|tun.*|tap.*"`

var builtinPresets = []Preset{
	builtin("tpl_node_up", "node", "Узел: сбор метрик", `up{source_id="$source_id"}`, "bool", ""),
	builtin("tpl_node_cpu", "node", "Узел: загрузка CPU", `100 * (1 - avg by (instance) (rate(node_cpu_seconds_total{source_id="$source_id",mode="idle"})))`, "percent", "", Threshold{Value: 70, Color: "warn"}, Threshold{Value: 90, Color: "crit"}),
	builtin("tpl_node_load1", "node", "Узел: load average 1m", `avg_over_time(node_load1{source_id="$source_id"})`, "count", ""),
	builtin("tpl_node_memory", "node", "Узел: занято RAM", `100 * (1 - avg_over_time(node_memory_MemAvailable_bytes{source_id="$source_id"}) / avg_over_time(node_memory_MemTotal_bytes{source_id="$source_id"}))`, "percent", "", Threshold{Value: 80, Color: "warn"}, Threshold{Value: 95, Color: "crit"}),
	builtin("tpl_node_memory_bytes", "node", "Узел: занято RAM, байт", `avg_over_time(node_memory_MemTotal_bytes{source_id="$source_id"}) - avg_over_time(node_memory_MemAvailable_bytes{source_id="$source_id"})`, "bytes", ""),
	builtin("tpl_node_fs", "node", "Узел: занятость файловых систем", `100 * (1 - max by (mountpoint) (node_filesystem_avail_bytes{source_id="$source_id",`+nodeFS+`}) / max by (mountpoint) (node_filesystem_size_bytes{source_id="$source_id",`+nodeFS+`}))`, "percent", "{{mountpoint}}", Threshold{Value: 80, Color: "warn"}, Threshold{Value: 90, Color: "crit"}),
	builtin("tpl_node_net_rx", "node", "Узел: сеть, приём", `sum by (device) (rate(node_network_receive_bytes_total{source_id="$source_id",`+nodeNet+`}))`, "bytes_per_second", "{{device}}"),
	builtin("tpl_node_net_tx", "node", "Узел: сеть, передача", `sum by (device) (rate(node_network_transmit_bytes_total{source_id="$source_id",`+nodeNet+`}))`, "bytes_per_second", "{{device}}"),
	builtin("tpl_node_uptime", "node", "Узел: время работы", `time() - max(node_boot_time_seconds{source_id="$source_id"})`, "seconds", ""),

	builtin("tpl_container_cpu", "container", "Контейнеры: CPU, ядер", `topk_max(10, sum by (name) (rate(container_cpu_usage_seconds_total{source_id="$source_id",name!=""})))`, "count", "{{name}}"),
	builtin("tpl_container_memory", "container", "Контейнеры: память", `topk_max(10, sum by (name) (avg_over_time(container_memory_working_set_bytes{source_id="$source_id",name!=""})))`, "bytes", "{{name}}"),
	builtin("tpl_container_net_rx", "container", "Контейнеры: сеть, приём", `topk_max(10, sum by (name) (rate(container_network_receive_bytes_total{source_id="$source_id",name!=""})))`, "bytes_per_second", "{{name}}"),
	builtin("tpl_container_net_tx", "container", "Контейнеры: сеть, передача", `topk_max(10, sum by (name) (rate(container_network_transmit_bytes_total{source_id="$source_id",name!=""})))`, "bytes_per_second", "{{name}}"),
	builtin("tpl_container_count", "container", "Контейнеры: запущено", `count(count by (name) (container_last_seen{source_id="$source_id",name!=""}))`, "count", ""),

	builtin("tpl_probe_success", "probe", "Сервис: успешность проверок", `100 * avg_over_time(homedeck_probe_success{service_id="$service_id"})`, "percent", ""),
	builtin("tpl_probe_latency", "probe", "Сервис: задержка проверки", `avg_over_time(homedeck_probe_duration_seconds{service_id="$service_id"})`, "seconds", ""),
	builtin("tpl_probe_last", "probe", "Сервис: последний результат", `homedeck_probe_success{service_id="$service_id"}`, "bool", ""),

	builtin("tpl_source_up", "homedeck", "Источник: доступность scrape", `up{source_id="$source_id"}`, "bool", ""),
	builtin("tpl_source_samples", "homedeck", "Источник: samples за scrape", `max_over_time(scrape_samples_scraped{source_id="$source_id"})`, "count", ""),
	builtin("tpl_source_duration", "homedeck", "Источник: длительность scrape", `max_over_time(scrape_duration_seconds{source_id="$source_id"})`, "seconds", ""),
	builtin("tpl_app_memory", "homedeck", "HomeDeck: память процесса", `max_over_time(process_resident_memory_bytes{source_id="homedeck"})`, "bytes", ""),
	builtin("tpl_app_cpu", "homedeck", "HomeDeck: CPU процесса", `rate(process_cpu_seconds_total{source_id="homedeck"})`, "count", ""),
	builtin("tpl_app_requests", "homedeck", "HomeDeck: HTTP-запросы", `sum(rate(homedeck_http_requests_total{source_id="homedeck"}))`, "per_second", ""),
	builtin("tpl_app_services_up", "homedeck", "Сервисы: доступно", `sum(homedeck_probe_success)`, "count", ""),

	builtin("tpl_tsdb_memory", "tsdb", "TSDB: память процесса", `max_over_time(process_resident_memory_bytes{source_id="tsdb"})`, "bytes", ""),
	builtin("tpl_tsdb_ingest", "tsdb", "TSDB: запись, samples/с", `sum(rate(vm_rows_inserted_total{source_id="tsdb"}))`, "per_second", ""),
	builtin("tpl_tsdb_series", "tsdb", "TSDB: активные ряды", `max_over_time(vm_cache_entries{source_id="tsdb",type="storage/hour_metric_ids"})`, "count", ""),
	builtin("tpl_tsdb_disk", "tsdb", "TSDB: объём данных", `sum(vm_data_size_bytes{source_id="tsdb"})`, "bytes", ""),
	builtin("tpl_tsdb_free", "tsdb", "TSDB: свободно на диске", `min(vm_free_disk_space_bytes{source_id="tsdb"})`, "bytes", ""),

	// Устройства: общие ключи величин одинаковы для любых драйверов (см. internal/devices).
	builtin("tpl_device_up", "device", "Устройство: на связи", `homedeck_device_up{device_id="$device_id"}`, "bool", ""),
	builtin("tpl_device_temperature", "device", "Устройство: температура", deviceValue("temperature"), "celsius", ""),
	builtin("tpl_device_humidity", "device", "Устройство: влажность", deviceValue("humidity"), "percent", ""),
	builtin("tpl_device_co2", "device", "Устройство: CO₂", deviceValue("co2"), "ppm", "", Threshold{Value: 1000, Color: "warn"}, Threshold{Value: 2000, Color: "crit"}),
	builtin("tpl_device_pm25", "device", "Устройство: PM2.5", deviceValue("pm25"), "ugm3", "", Threshold{Value: 35, Color: "warn"}, Threshold{Value: 75, Color: "crit"}),
	builtin("tpl_device_pm10", "device", "Устройство: PM10", deviceValue("pm10"), "ugm3", "", Threshold{Value: 50, Color: "warn"}, Threshold{Value: 100, Color: "crit"}),
	builtin("tpl_device_formaldehyde", "device", "Устройство: формальдегид", deviceValue("formaldehyde"), "mgm3", "", Threshold{Value: 0.08, Color: "warn"}, Threshold{Value: 0.1, Color: "crit"}),
	builtin("tpl_device_battery", "device", "Устройство: заряд батареи", deviceValue("battery"), "percent", ""),
	builtin("tpl_device_values", "device", "Устройство: все значения", `homedeck_device_value{device_id="$device_id"}`, "", "{{key}}"),
}

func deviceValue(key string) string {
	return `homedeck_device_value{device_id="$device_id",key="` + key + `"}`
}

func builtin(id, category, title, expr, unit, legend string, th ...Threshold) Preset {
	if th == nil {
		th = []Threshold{}
	}
	return Preset{
		ID:       id,
		Builtin:  true,
		Category: category,
		Vars:     PresetVarsOf(expr),
		Revision: 1,
		Title:    title, Expression: expr, Unit: unit, Legend: legend, Thresholds: th,
	}
}

func BuiltinPresets() []Preset {
	out := make([]Preset, len(builtinPresets))
	copy(out, builtinPresets)
	return out
}

func BuiltinPreset(id string) (Preset, bool) {
	for _, p := range builtinPresets {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}

func IsBuiltinPresetID(id string) bool { return strings.HasPrefix(id, "tpl_") }

func LegendName(tpl string, labels map[string]string) string {
	if tpl == "" {
		keys := slices.Sorted(maps.Keys(labels))
		var parts []string
		for _, k := range keys {
			if k != "__name__" && k != "job" && k != "source_id" && k != "instance" {
				parts = append(parts, k+"="+labels[k])
			}
		}
		switch {
		case len(parts) > 0:
			return strings.Join(parts, ", ")
		case labels["instance"] != "":
			return labels["instance"]
		case labels["__name__"] != "":
			return labels["__name__"]
		}
		return "значение"
	}
	var b strings.Builder
	for {
		i := strings.Index(tpl, "{{")
		if i < 0 {
			b.WriteString(tpl)
			break
		}
		j := strings.Index(tpl[i:], "}}")
		if j < 0 {
			b.WriteString(tpl)
			break
		}
		b.WriteString(tpl[:i])
		b.WriteString(labels[strings.TrimSpace(tpl[i+2:i+j])])
		tpl = tpl[i+j+2:]
	}
	return b.String()
}
