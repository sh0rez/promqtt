![](./logo.svg)

# Promqtt: Prometheus ⟷ MQTT Bridge

Promqtt makes Prometheus MQTT capable in a truly generic way.

It has no assumptions on message payloads or topic layout.

## Installation

Promqtt is best used with Docker, an image is available at [`shorez/promqtt`](https://hub.docker.com/r/shorez/promqtt).

If that is no option, Promqtt can also be installed from source using Go:

```sh
$ go install github.com/sh0rez/promqtt@latest
```

Promqtt accepts very little configuration which is exposed as command line flags:

```bash
promqtt <broker> [flags]
```

**Arguments**:

- `broker`: Any MQTT broker (e.g. `myBroker:1883`)

**Flags**:

- `--client-id`: The MQTT Client ID to use. Defaults to `promqtt@HOSTNAME`
- `--listen`: The address to bind the HTTP server to. Defaults to `:9337`

Above list may be incomplete, consult `promqtt --help` for what your current build supports

## Usage

In similar fashion to the `blackbox_exporter` and `snmp_exporter`, Promqtt acts
as a general purpose relay, meaning you do not configure specific topics, etc.
ahead of time.

Instead, these must be provided by Prometheus while scraping. Using clever
relabeling it makes Prometheus look like it supported MQTT itself:

### Basic

The most basic use-case assumes a sender literally publishes `float64`
compatible values (any kind of number really) to some MQTT topic:

> **Topic**: `diox/06FE2A3/co2`  
> **Payload**: `42.0`

A possible `scrape_configs` entry for looks like this:

```yaml
- job_name: diox
  metrics_path: /mqtt # Promqtt serves MQTT metrics under this path
  static_configs:
    - targets:
        - diox/.+/.+ # regular expression matching some MQTT topic
  relabel_configs:
    # copy above address (diox/...) into the "topic" URL parameter
    - source_labels: [__address__]
      target_label: __param_topic
    # and also to the instance label
    - source_labels: [__param_topic]
      target_label: instance
    # make Prometheus scrape Promqtt at ADDRESS
    - target_label: __address__
      replacement: ADDRESS:9337
```

Considering above MQTT message, this would yield the following series:

```bash
# TYPE diox_06FE2A3_co2 gauge
diox_06FE2A3_co2{topic="diox/06FE2A3/co2", instance="diox/.+/.+"} 42.0
```

### Regex

Obviously, not all MQTT devices publish perfect float values to distinct topics.
To accompany for that, Promqtt lets you optionally a regular expression using
named capture groups to extract parts of messages:

> **Topic**: `tele/tv/SENSOR`  
> **Payload**:
>
> ```js
> {"Time":"2021-06-27T17:38:56","ENERGY":{"TotalStartTime":"2021-01-16T13:12:08","Total":56.040,"Yesterday":0.923,"Today":0.536,"Period":1,"Power":10,"ApparentPower":32,"ReactivePower":30,"Factor":0.32,"Voltage":233,"Current":0.136}}
> ```

We could use the following regular expression, to parse out the `Power`,
`Voltage` and `Current` fields
([Explanation](https://regex101.com/r/igWHKh/1/)):

```regex
"Power":(?P<_power>[\d.]+).*"Voltage":(?P<_voltage>[\d.]+).*"Current":(?P<_current>[\d.]+)
```

This gives us the following `scrape_configs` entry:

```yaml
- job_name: tasmota
  metrics_path: /mqtt # Promqtt serves MQTT metrics under this path
  static_configs:
    - targets:
        - tele/.+/SENSOR # regular expression matching some MQTT topic
  params:
    regex:
      - '"Total": *(?P<_total_kWh>[\d.]+).*"Power": *(?P<_power_watt>[\d.]+).*"ApparentPower": *(?P<_apparent_power_VA>[\d.]+).*"ReactivePower": *(?P<_reactive_power_VAr>[\d.]+).*"Factor": *(?P<_factor>[\d.]+).*"Voltage": *(?P<_voltage>[\d.]+).*"Current": *(?P<_current>[\d.]+)'

  relabel_configs:
    # copy above address (tele/...) into the "topic" URL parameter
    - source_labels: [__address__]
      target_label: __param_topic
    # and also to the instance label
    - source_labels: [__param_topic]
      target_label: instance
    # make Prometheus scrape Promqtt at ADDRESS
    - target_label: __address__
      replacement: ADDRESS:9337
```

The names of the capture groups (`_power`, `_voltage` and `_current`) are
appended to the generated series name, yielding the following:

```bash
# TYPE tele_tv_SENSOR_power gauge
tele_tv_SENSOR_power{topic="tele/tv/SENSOR", instance="tele/.+/SENSOR"} 10

# TYPE tele_tv_SENSOR_voltage gauge
tele_tv_SENSOR_voltage{topic="tele/tv/SENSOR", instance="tele/.+/SENSOR"} 233

# TYPE tele_tv_SENSOR_current gauge
tele_tv_SENSOR_current{topic="tele/tv/SENSOR", instance="tele/.+/SENSOR"} 0.136
```

### Series renaming

Above series obviously don't quite comply with the [Prometheus naming
conventions](https://prometheus.io/docs/practices/naming/).

But again, this can be fixed by adding some `metric_relabel_configs`. Continuing above example:

```yaml
- job_name: tasmota
  # ...
  metric_relabel_configs:
    # rename series to tasmota_<field> style
    - source_labels: [__name__]
      target_label: __name__
      regex: tele.+_SENSOR_(.+)
      replacement: tasmota_$1
    # extract device from topic to its own label
    - source_labels: [topic]
      target_label: dev
      regex: tele/(.+)/.+
```

Afterwards, we get the following:

```bash
# TYPE tele_tv_SENSOR_power gauge
tasmota_power{topic="tele/tv/SENSOR", instance="tele/.+/SENSOR", dev="tv"} 10

# TYPE tele_tv_SENSOR_voltage gauge
tasmota_voltage{topic="tele/tv/SENSOR", instance="tele/.+/SENSOR", dev="tv"} 233

# TYPE tele_tv_SENSOR_current gauge
tasmota_current{topic="tele/tv/SENSOR", instance="tele/.+/SENSOR", dev="tv"} 0.136
```
