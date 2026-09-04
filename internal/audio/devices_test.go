package audio

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

const sinkInspect = `id 82, type PipeWire:Interface:Node
    device.profile.description = "Speaker"
  * media.class = "Audio/Sink"
  * node.description = "WH-1000XM5"
  * node.name = "bluez_output.58_18_62_16_46_A7.1"
  * node.nick = "WH-1000XM5"
`

const sourceInspect = `id 74, type PipeWire:Interface:Node
  * media.class = "Audio/Source"
  * node.description = "Scarlett 2i2 USB"
  * node.name = "alsa_input.usb-Focusrite_Scarlett_2i2"
`

type fakeRunner struct {
	outputs map[string][]byte
	err     error
}

func (f fakeRunner) Output(_ context.Context, _ string, args ...string) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.outputs[args[len(args)-1]], nil
}

func TestParseInspect(t *testing.T) {
	got, err := ParseInspect([]byte(sinkInspect))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"device.profile.description": "Speaker",
		"media.class":                "Audio/Sink",
		"node.description":           "WH-1000XM5",
		"node.name":                  "bluez_output.58_18_62_16_46_A7.1",
		"node.nick":                  "WH-1000XM5",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseInspect() = %#v, want %#v", got, want)
	}
}

func TestDiscover(t *testing.T) {
	devices, err := Discover(context.Background(), fakeRunner{outputs: map[string][]byte{
		DefaultSinkAlias:   []byte(sinkInspect),
		DefaultSourceAlias: []byte(sourceInspect),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if devices.Output.NodeName != "bluez_output.58_18_62_16_46_A7.1" {
		t.Fatalf("unexpected sink: %#v", devices.Output)
	}
	if devices.Microphone.Description != "Scarlett 2i2 USB" {
		t.Fatalf("unexpected microphone: %#v", devices.Microphone)
	}
}

func TestDiscoverReportsRunnerFailure(t *testing.T) {
	_, err := Discover(context.Background(), fakeRunner{err: errors.New("wireplumber unavailable")})
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestParseAudioNodeIDsIncludesFilterSources(t *testing.T) {
	status := `PipeWire 'pipewire-0'
Audio
 ├─ Sinks:
 │  *   82. alsa_output.speaker [vol: 1.00]
 ├─ Sources:
 │      74. alsa_input.internal [vol: 1.00]
 ├─ Filters:
 │      79. bluez_input.headset [Audio/Source]
 │      80. ignored.stream [Stream/Input/Audio/Internal]
 └─ Streams:
        64. Firefox
            119. output_FL > speaker:playback_FL
Video
`
	want := []int{82, 74, 79, 80, 64, 119}
	if got := ParseAudioNodeIDs([]byte(status)); !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseAudioNodeIDs() = %#v, want %#v", got, want)
	}
}

func TestDiscoverAvailableAndResolveSelection(t *testing.T) {
	usbSource := `id 79, type PipeWire:Interface:Node
  * media.class = "Audio/Source"
  * node.description = "USB microphone"
  * node.name = "alsa_input.usb"
`
	hdmiSink := `id 83, type PipeWire:Interface:Node
  * media.class = "Audio/Sink"
  * node.description = "HDMI output"
  * node.name = "alsa_output.hdmi"
`
	stream := `id 64, type PipeWire:Interface:Node
  * media.class = "Stream/Output/Audio"
  * node.name = "firefox"
`
	runner := fakeRunner{outputs: map[string][]byte{
		DefaultSinkAlias:   []byte(sinkInspect),
		DefaultSourceAlias: []byte(sourceInspect),
		"-n":               []byte("Audio\n  79. source\n  83. sink\n  64. stream\nVideo\n"),
		"79":               []byte(usbSource),
		"83":               []byte(hdmiSink),
		"64":               []byte(stream),
	}}
	inventory, err := DiscoverAvailable(context.Background(), runner)
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Microphones) != 2 || len(inventory.Outputs) != 2 {
		t.Fatalf("unexpected inventory: %#v", inventory)
	}
	selected, err := ResolveSelection(context.Background(), runner, "alsa_input.usb", "alsa_output.hdmi")
	if err != nil {
		t.Fatal(err)
	}
	if selected.Microphone.Description != "USB microphone" || selected.Output.Description != "HDMI output" {
		t.Fatalf("unexpected selection: %#v", selected)
	}
	if _, err := ResolveSelection(context.Background(), runner, "missing", ""); err == nil {
		t.Fatal("expected unavailable selection to fail")
	}
}
