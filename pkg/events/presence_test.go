package events

import "testing"

func TestSynthesizePresencePriority(t *testing.T) {
	now := int64(1700000000000)

	// InMeeting overrides everything, even if WS connection dropped transiently.
	rec := SynthesizePresence("u1", false, PresenceAway, "room_1", now)
	if rec.Status != PresenceInMeeting || rec.MeetingID != "room_1" {
		t.Fatalf("expected in_meeting, got %+v", rec)
	}

	// Offline when no conn and no meeting.
	rec = SynthesizePresence("u1", false, PresenceOnline, "", now)
	if rec.Status != PresenceOffline || rec.MeetingID != "" {
		t.Fatalf("expected offline, got %+v", rec)
	}

	// Away when conn alive and manualState == away.
	rec = SynthesizePresence("u1", true, PresenceAway, "", now)
	if rec.Status != PresenceAway {
		t.Fatalf("expected away, got %+v", rec)
	}

	// Online default when conn alive.
	rec = SynthesizePresence("u1", true, "", "", now)
	if rec.Status != PresenceOnline {
		t.Fatalf("expected online, got %+v", rec)
	}
}
