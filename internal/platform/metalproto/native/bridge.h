#ifndef NANOMETAL_BRIDGE_H
#define NANOMETAL_BRIDGE_H
#include <stdint.h>
typedef struct { double submit, encode, wait, drawable_wait, gpu_start, gpu_end, completed, presented; uint64_t memory; int32_t status, reserved; } NMRow;
void* nm_create(int width,int height,int offscreen,int vsync,int effects,int capacity,const char* shader,char* error,int error_size);
int nm_mesh(void* renderer,const void* vertices,int vertex_count,const uint32_t* indices,int index_count,const uint32_t* offsets,int instances);
int nm_texture(void* renderer,int slot,int width,int height,const void* rgba);
int nm_configure(void* renderer,const float* camera,const float* rect,int pose_count);
int nm_frame(void* renderer,const void* previous,const void* next,float alpha,double seconds);
void nm_drain(void* renderer);
int nm_rows(void* renderer,void* rows,int capacity);
int nm_capture(void* renderer,const char* path);
void nm_info(void* renderer,char* output,int capacity);
void nm_destroy(void* renderer);
#endif
