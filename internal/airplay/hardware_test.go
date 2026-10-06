package airplay

import "testing"

func TestClassifyHardware(t *testing.T) {
	all := func(string) bool { return true }
	none := func(string) bool { return false }
	cases := []struct {
		name string
		in   hwInfo
		want Hardware
	}{
		{
			name: "nvidia proprietary",
			in:   hwInfo{nvidia: true, pluginAvail: all},
			want: Hardware{Decoder: "nvh264dec", Sink: "glimagesink"},
		},
		{
			name: "raspberry pi 4 v4l2",
			in:   hwInfo{model: "Raspberry Pi 4 Model B Rev 1.4", pluginAvail: all},
			want: Hardware{Decoder: "v4l2h264dec", Converter: "v4l2convert", Sink: "waylandsink"},
		},
		{
			name: "raspberry pi 5 has no h264 decoder",
			in:   hwInfo{model: "Raspberry Pi 5 Model B Rev 1.0", pluginAvail: all},
			want: Hardware{},
		},
		{
			name: "intel vaapi",
			in:   hwInfo{driVendors: []string{"0x8086"}, pluginAvail: all},
			want: Hardware{Decoder: "vah264dec", Sink: "waylandsink"},
		},
		{
			name: "amd vaapi",
			in:   hwInfo{driVendors: []string{"0x1002"}, pluginAvail: all},
			want: Hardware{Decoder: "vah264dec", Sink: "waylandsink"},
		},
		{
			name: "vaapi legacy fallback",
			in: hwInfo{
				driVendors:  []string{"0x8086"},
				pluginAvail: func(el string) bool { return el == "vaapih264dec" },
			},
			want: Hardware{Decoder: "vaapih264dec", Sink: "waylandsink"},
		},
		{
			name: "no gpu detected",
			in:   hwInfo{pluginAvail: all},
			want: Hardware{},
		},
		{
			name: "decoder plugin missing",
			in:   hwInfo{driVendors: []string{"0x8086"}, pluginAvail: none},
			want: Hardware{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classify(tc.in)
			if got != tc.want {
				t.Errorf("classify() = %+v, want %+v", got, tc.want)
			}
		})
	}
}
