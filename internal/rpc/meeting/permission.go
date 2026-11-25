package meeting

import (
	"context"
	pbmeeting "github.com/openimsdk/protocol/openmeeting/meeting"
	"github.com/openimsdk/tools/errs"
)

func (s *meetingServer) checkAuthPermission(creatorUserID, hostUserID, requestUserID string) bool {
	return hostUserID == requestUserID || creatorUserID == requestUserID
}

func (s *meetingServer) checkUserEnableCamera(setting *pbmeeting.MeetingSetting, personalData *pbmeeting.PersonalData) bool {
	if setting.CanParticipantsEnableCamera && personalData.PersonalSetting.CameraOnEntry && personalData.LimitSetting.CameraOnEntry {
		return true
	}
	return false
}

func (s *meetingServer) checkUserEnableMicrophone(setting *pbmeeting.MeetingSetting, personalData *pbmeeting.PersonalData) bool {
	// only when three condition enable can turn on the microphone
	if setting.CanParticipantsUnmuteMicrophone && personalData.PersonalSetting.MicrophoneOnEntry && personalData.LimitSetting.MicrophoneOnEntry {
		return true
	}
	return false
}

func (s *meetingServer) checkUserInMeeting(ctx context.Context, userID string) (bool, error) {
	// Fast O(1) path via Redis presence write-through + LiveKit Webhook.
	if s.eventHub != nil {
		cachedRoomID, err := s.eventHub.GetUserInMeeting(ctx, userID)
		if err == nil {
			if cachedRoomID == "" {
				return false, nil
			}
			// Verify cached room still contains the user; auto-heal stale presence if absent.
			if s.meetingRtc == nil {
				return true, nil
			}
			userIDs, lErr := s.meetingRtc.GetParticipantUserIDs(ctx, cachedRoomID)
			if lErr != nil {
				_ = s.eventHub.ClearUserInMeeting(ctx, userID, cachedRoomID)
				return false, nil
			}
			for _, id := range userIDs {
				if id == userID {
					return true, nil
				}
			}
			_ = s.eventHub.ClearUserInMeeting(ctx, userID, cachedRoomID)
			return false, nil
		}
	}

	if s.meetingRtc == nil {
		return false, nil
	}
	rooms, err := s.meetingRtc.GetAllRooms(ctx)
	if err != nil {
		return true, err
	}

	for _, room := range rooms {
		userIDs, err := s.meetingRtc.GetParticipantUserIDs(ctx, room.Name)
		if err != nil {
			return true, errs.WrapMsg(err, "get participants failed")
		}
		// check if user is already in meeting
		for _, userIdentity := range userIDs {
			if userIdentity == userID {
				return true, nil
			}
		}
	}
	return false, nil
}
