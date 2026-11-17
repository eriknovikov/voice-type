#ifndef VOICE_TYPE_PERMISSIONS_H
#define VOICE_TYPE_PERMISSIONS_H

int vt_microphone_status(void);
int vt_request_microphone(void);
int vt_accessibility_trusted(void);
void vt_request_accessibility(void);
void vt_open_permission_settings(int microphone);

#endif
