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
