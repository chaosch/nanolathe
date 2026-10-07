#ifndef NANOMETAL_BRIDGE_H
#define NANOMETAL_BRIDGE_H
#include <stdint.h>
typedef struct { double submit, encode, wait, drawable_wait, gpu_start, gpu_end, completed, presented; uint64_t memory; int32_t status, reserved; } NMRow;
// Positive capacity retains every finite sample; negative capacity is a
// circular interactive history of its absolute value.
void* nm_create(int width,int height,int offscreen,int vsync,int effects,int visual_passes,int capacity,const char* shader,char* error,int error_size);
int nm_mesh(void* renderer,const void* vertices,int vertex_count,const uint32_t* indices,int index_count,const uint32_t* offsets,int instances,const uint32_t* edges,int edge_count,const void* visuals);
typedef struct { const void *materials,*frames,*heights; uint32_t material_count,frame_count,width,height;float rect[4]; } NMAssets;
int nm_assets(void* renderer,const NMAssets* assets);
int nm_texture(void* renderer,int slot,int width,int height,const void* rgba);
int nm_configure(void* renderer,const float* camera,const float* rect,int pose_count);
int nm_frame(void* renderer,const void* previous,const void* next,float alpha,double seconds);
// Pointer-only upload descriptor; all data is copied before this call returns.
typedef struct {
 const void *previous,*current,*offsets,*spans,*sprites,*lights,*visuals,*distortions,*fog,*selectors,*overlay;
 float camera[4],fog_rect[4];
 float alpha;
 uint32_t pose_count,instance_count,sprite_count,light_count,distortion_count,fog_width,fog_height,overlay_count;
 double seconds;
 const void *overlay_texture;
 uint64_t overlay_version;
 uint32_t overlay_width,overlay_height,overlay_dirty[4];
} NMLiveUpload;
typedef struct { float pan_x,pan_z,zoom;uint32_t pause_toggle; } NMInput;
// Ordered events use top-left physical drawable pixels; shared lane meanings
// extend meshscene.NativeEvent as documented in native/README.md. Wheel move
// records carry signed integer zoom notches in button; text/focus/pinch kinds
// are 7/8/9. Key events carry the Cocoa virtual keycode in button.
typedef struct { uint32_t kind,key,button,modifiers; float x,y,wheel_x,wheel_y; } NMEvent;
void nm_playable(void* renderer,int playable);
int nm_events(void* renderer,NMEvent* events,int capacity);
void nm_input(void* renderer,NMInput* input);
int nm_live_frame(void* renderer,const NMLiveUpload* upload);
void nm_drain(void* renderer);
int nm_rows(void* renderer,void* rows,int capacity);
int nm_capture(void* renderer,const char* path);
void nm_info(void* renderer,char* output,int capacity);
void nm_destroy(void* renderer);
#endif
