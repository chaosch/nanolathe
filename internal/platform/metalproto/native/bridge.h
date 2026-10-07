#ifndef NANOMETAL_BRIDGE_H
#define NANOMETAL_BRIDGE_H
#include <stdint.h>
#include "effects.h"
#include "ground_effects.h"
#include "shadows.h"
typedef struct { double submit, encode, wait, drawable_wait, gpu_start, gpu_end, completed, presented; uint64_t memory; int32_t status, reserved; } NMRow;
// Positive capacity retains every finite sample; negative capacity is a
// circular interactive history of its absolute value.
void* nm_create(int width,int height,int offscreen,int vsync,int effects,int visual_passes,int capacity,const char* shader,char* error,int error_size);
int nm_mesh(void* renderer,const void* vertices,int vertex_count,const uint32_t* indices,int index_count,const uint32_t* offsets,int instances,const uint32_t* edges,int edge_count,const void* visuals,const void* primitive_centers,int primitive_count);
typedef struct { const void *materials,*frames,*heights; uint32_t material_count,frame_count,width,height;float rect[4]; } NMAssets;
int nm_assets(void* renderer,const NMAssets* assets);
int nm_texture(void* renderer,int slot,int width,int height,const void* rgba);
int nm_configure(void* renderer,const float* camera,const float* rect,int pose_count);
int nm_frame(void* renderer,const void* previous,const void* next,float alpha,double seconds);
typedef struct { uint32_t mesh,instance,group,phase; } NMCloakDraw;
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
 uint32_t ground_sprite_count;
 const NMCloakDraw* cloaks;
 uint32_t cloak_count,air_count;
 float water[20];
 const void *rules,*material_offsets,*material_overrides;
 uint32_t rules_count,material_count;
 const void *composition_slots,*composition_selectors,*composition_draws,*composition_paint;
 uint32_t composition_slot_count,composition_draw_count,composition_paint_count,composition_width,composition_height,composed;
 const void* retained_lights;uint32_t retained_light_count,light_controls;
 const NMEffectsUpload* effects;
 const void* glow_vertices;uint32_t glow_count,glow_enabled;float glow_parameters[12];
 const NMGroundUpload* ground_effects;
 NMShadowUpload shadows;
 const void* water_objects;
 const void* water_sources;
 const void* groups;
 uint32_t terrain_flags;
 const void* projections;
 // Per-slot rectangles drawn in the slot's paint band before its body:
 // {x,y,w,h,r,g,b,a} physical pixels; ranges are {first,count} per slot.
 const void* annotations;const uint32_t* annotation_ranges;uint32_t annotation_count,annotation_range_count;
} NMLiveUpload;
typedef struct { float pan_x,pan_z,zoom;uint32_t pause_toggle; } NMInput;
// Ordered events use top-left physical drawable pixels; shared lane meanings
// extend meshscene.NativeEvent as documented in native/README.md. Wheel move
// records carry signed integer zoom notches in button; text/focus/pinch kinds
// are 7/8/9. Key events carry the Cocoa virtual keycode in button.
typedef struct { uint32_t kind,key,button,modifiers; float x,y,wheel_x,wheel_y; } NMEvent;
void nm_playable(void* renderer,int playable);
// Capture delivers cumulative virtual drawable coordinates; release restores
// the pre-capture content-view position. Returns zero on a CoreGraphics error.
int nm_pointer_capture(void* renderer,int captured);
int nm_events(void* renderer,NMEvent* events,int capacity);
void nm_input(void* renderer,NMInput* input);
int nm_terrain_tiles(void* renderer,const void* upload);
// Upload copies everything the frame reads; present acquires the drawable,
// encodes and commits. Upload must be followed by exactly one present.
int nm_live_upload(void* renderer,const NMLiveUpload* upload);
int nm_live_present(void* renderer);
void nm_drain(void* renderer);
int nm_rows(void* renderer,void* rows,int capacity);
int nm_capture(void* renderer,const char* path);
void nm_info(void* renderer,char* output,int capacity);
void nm_destroy(void* renderer);
#endif
