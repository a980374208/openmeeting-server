package meeting

import (
	"context"
	"encoding/json"

	"github.com/openimsdk/openmeeting-server/pkg/common/constant"
	"github.com/openimsdk/openmeeting-server/pkg/common/servererrs"
	pbmeeting "github.com/openimsdk/protocol/openmeeting/meeting"
	"github.com/openimsdk/tools/errs"
)

// validateCatalogMetadata rejects unknown ownership instead of treating the
// creator as host. Do not include raw metadata in errors: it contains secrets.
func validateCatalogMetadata(meetingID string, metadata *pbmeeting.MeetingMetadata) error {
	if meetingID == "" || metadata == nil || metadata.Detail == nil ||
		metadata.Detail.Info == nil || metadata.Detail.Info.SystemGenerated == nil ||
		metadata.Detail.Info.CreatorDefinedMeeting == nil ||
		metadata.Detail.Info.SystemGenerated.MeetingID != meetingID ||
		metadata.Detail.Info.CreatorDefinedMeeting.HostUserID == "" {
		return servererrs.ErrInternalServer.WrapMsg("current meeting ownership is unavailable")
	}
	return nil
}

func (s *meetingServer) getHostedInProgressMeetings(ctx context.Context, userID string) ([]*pbmeeting.MeetingInfoSetting, error) {
	rooms, err := s.meetingRtc.GetAllRooms(ctx)
	if err != nil {
		return nil, errs.WrapMsg(err, "current meeting ownership is unavailable")
	}
	var details []*pbmeeting.MeetingInfoSetting
	for _, room := range rooms {
		if room == nil || room.Name == "" {
			return nil, servererrs.ErrInternalServer.WrapMsg("current meeting ownership is unavailable")
		}
		metadata := &pbmeeting.MeetingMetadata{}
		metadataErr := json.Unmarshal([]byte(room.Metadata), metadata)
		if metadataErr == nil {
			metadataErr = validateCatalogMetadata(room.Name, metadata)
		}
		if metadataErr != nil {
			// A shared LiveKit instance can contain rooms from other products.
			// Only malformed rooms need this extra lookup. An existing ongoing
			// business meeting with unknown ownership must fail closed.
			info, err := s.meetingStorageHandler.TakeCatalog(ctx, room.Name)
			if err != nil {
				if errs.ErrRecordNotFound.Is(err) {
					continue
				}
				return nil, errs.WrapMsg(err, "check meeting catalog record failed")
			}
			if info == nil || info.MeetingID != room.Name {
				return nil, servererrs.ErrInternalServer.WrapMsg("invalid meeting catalog record")
			}
			if info.Status != constant.InProgress {
				continue
			}
			return nil, servererrs.ErrInternalServer.WrapMsg("current meeting ownership is unavailable")
		}
		if metadata.Detail.Info.CreatorDefinedMeeting.HostUserID != userID {
			continue
		}

		// Fetch only this user's hosted rooms; creator_user_id remains audit data.
		info, err := s.meetingStorageHandler.TakeCatalog(ctx, room.Name)
		if err != nil {
			if errs.ErrRecordNotFound.Is(err) {
				// The room may have been deleted after the LiveKit snapshot, or
				// creation may not yet have committed its database record.
				continue
			}
			return nil, errs.WrapMsg(err, "get hosted meeting data failed")
		}
		if info == nil || info.MeetingID != room.Name {
			return nil, servererrs.ErrInternalServer.WrapMsg("invalid meeting catalog record")
		}
		if info.Status != constant.InProgress {
			continue
		}
		detail, err := s.getMeetingDetailSettingWithMetadata(ctx, info, metadata)
		if err != nil {
			return nil, err
		}
		details = append(details, detail)
	}
	return details, nil
}
