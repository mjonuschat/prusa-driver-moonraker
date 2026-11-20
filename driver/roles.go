package moonraker

import "strings"

type roleOptions struct {
	chamber, bedFans, partFan, heatbreakFan, filament, chamberLight, chamberFan string
	extraSensors, extraFans                                                     []string
}

func roleOptionsOf(options map[string]string) roleOptions {
	return roleOptions{
		chamber:      options["chamber_object"],
		bedFans:      options["bed_fans_object"],
		partFan:      options["part_fan_object"],
		heatbreakFan: options["heatbreak_fan_object"],
		filament:     options["filament_object"],
		chamberLight: options["chamber_light_object"],
		chamberFan:   options["chamber_fan_object"],
		extraSensors: splitList(options["extra_sensors"]),
		extraFans:    splitList(options["extra_fans"]),
	}
}

func splitList(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" && !inList(out, part) {
			out = append(out, part)
		}
	}
	return out
}

type chamberRole struct {
	object string
	heater bool
}

type unresolved struct{ option, value string }

type roles struct {
	chamber                       chamberRole
	bedFan, partFan, heatbreakFan string
	filament                      string
	light                         string
	chamberFan                    string
	extraSensors, extraFans       []string
	unresolved                    []unresolved
}

func inList(list []string, object string) bool {
	for _, item := range list {
		if strings.EqualFold(item, object) {
			return true
		}
	}
	return false
}

// heaterName is the name SET_HEATER_TEMPERATURE HEATER= takes.
func heaterName(object string) string {
	if i := strings.LastIndex(object, " "); i >= 0 {
		return object[i+1:]
	}
	return object
}

func find(list []string, match func(string) bool) string {
	for _, item := range list {
		if match(item) {
			return item
		}
	}
	return ""
}

func named(list []string, name string) string {
	return find(list, func(o string) bool { return strings.EqualFold(o, name) })
}

// lookup matches a user-given value to a listed object by its full name, or
// by the part after the first space when exactly one listed object has it.
func lookup(listed []string, value string) string {
	if o := named(listed, value); o != "" {
		return o
	}
	found := ""
	for _, o := range listed {
		if _, rest, ok := strings.Cut(o, " "); ok && strings.EqualFold(rest, value) {
			if found != "" {
				return ""
			}
			found = o
		}
	}
	return found
}

func resolveRoles(listed, heaters []string, opts roleOptions) roles {
	var r roles
	// override returns (object, overridden).
	override := func(option, value string) (string, bool) {
		value = strings.TrimSpace(value)
		if value == "" {
			return "", false
		}
		if strings.EqualFold(value, "none") {
			return "", true
		}
		o := lookup(listed, value)
		if o == "" {
			r.unresolved = append(r.unresolved, unresolved{option, value})
		}
		return o, true
	}

	if o, ok := override("chamber_object", opts.chamber); ok {
		r.chamber.object = o
	} else {
		r.chamber.object = defaultChamber(listed, heaters)
	}
	r.chamber.heater = r.chamber.object != "" && inList(heaters, r.chamber.object)

	if o, ok := override("bed_fans_object", opts.bedFans); ok {
		r.bedFan = o
	} else if o := named(listed, "fan_generic bed_fans"); o != "" {
		r.bedFan = o
	} else if o := named(listed, "heater_generic bed_fans"); o != "" && r.chamber.heater {
		r.bedFan = o
	}
	if r.bedFan != "" && strings.EqualFold(r.bedFan, r.chamber.object) {
		r.bedFan = ""
	}

	if o, ok := override("part_fan_object", opts.partFan); ok {
		r.partFan = o
	} else {
		r.partFan = named(listed, "fan")
	}
	if o, ok := override("heatbreak_fan_object", opts.heatbreakFan); ok {
		r.heatbreakFan = o
	} else {
		r.heatbreakFan = named(listed, "heater_fan hotend_fan")
	}
	if o, ok := override("filament_object", opts.filament); ok {
		r.filament = o
	} else {
		r.filament = find(listed, func(o string) bool {
			return strings.HasPrefix(strings.ToLower(o), "filament_switch_sensor ")
		})
	}

	if o, ok := override("chamber_light_object", opts.chamberLight); ok {
		r.light = o
	} else {
		r.light = defaultLight(listed)
	}

	if o, ok := override("chamber_fan_object", opts.chamberFan); ok {
		r.chamberFan = o
	} else {
		r.chamberFan = defaultChamberFan(listed)
	}

	extras := func(option string, values []string) []string {
		var out []string
		for _, v := range values {
			o := lookup(listed, v)
			if o == "" {
				r.unresolved = append(r.unresolved, unresolved{option, v})
			} else if !inList(out, o) {
				out = append(out, o)
			}
		}
		return out
	}
	r.extraSensors = extras("extra_sensors", opts.extraSensors)
	r.extraFans = extras("extra_fans", opts.extraFans)
	return r
}

func defaultChamber(listed, heaters []string) string {
	for _, name := range []string{"chamber", "heater_chamber", "chamber_heater"} {
		if o := find(heaters, func(h string) bool { return strings.EqualFold(heaterName(h), name) }); o != "" {
			return o
		}
	}
	if o := named(heaters, "heater_generic bed_fans"); o != "" {
		return o
	}
	return named(listed, "temperature_sensor Chamber")
}

func (r roles) objects() []string {
	var out []string
	add := func(o string) {
		if o != "" && !inList(out, o) {
			out = append(out, o)
		}
	}
	for _, o := range []string{r.chamber.object, r.bedFan, r.partFan, r.heatbreakFan, r.filament, r.light, r.chamberFan} {
		add(o)
	}
	for _, o := range r.extraSensors {
		add(o)
	}
	for _, o := range r.extraFans {
		add(o)
	}
	return out
}
