#include <metal_stdlib>
using namespace metal;

// Static authored rings and rigid poses are the only geometry inputs. No CPU
// posed-corner stream or per-frame host vertex expansion (GPU design §11.2).
struct nm_face_Corner {packed_float3 p,n;float2 uv;packed_float3 color;uint piece,material;packed_float3 shadeNormal;};
struct nm_face_Face {uint cornerStart,cornerCount,piece,material,indexStart,indexCount,primitive,flags;};
struct nm_face_Material {uint firstFrame,frameCount,kind,reserved;};
struct nm_face_Selected {float4 uv,color,params;};
struct nm_face_Visual {float4 state,bounds,outline,emission;};
struct nm_face_Rules {float4 reveal,below,band,above,clip,shadow,outline;};
struct nm_face_Slot {float4 screen,atlas,params;};
struct nm_face_Control {uint4 span;float4 texture;};
struct nm_face_Prepared {int4 x,y,key,shade,step;float4 u,v,pixel;uint4 control;float4 centroid;};
// u/v: selected-frame texel coordinates of each rotated mapped corner. A corner's
// authored UV picks any region of its frame; retail's default corners are
// (0,0) (1,0) (1,1) (0,1) and reproduce the frame's endpoint lanes exactly.
// control: bottom's rotated index, original top index, flags, corner count.
// flags: admitted=1, quad mapping=2, shaded=4, unsupported coordinate=8, above-water corner=16.
// x/y use the production +8192 bias, making signed division the arithmetic
// shift's floor; bounds and horizontal span preserve signed 32-bit edge terms.

inline float2 nm_face_screen(float3 w,constant float4* u) {
 return float2(w.x-u[0].x,w.z-u[0].y-w.y*(u[4].x>.5?.5:1))*u[0].z+u[2].xy*.5;
}
inline float2 nm_face_pixel(float2 screen,nm_face_Prepared f) {return floor(screen*f.pixel.xy+f.pixel.zw)+.5;}
// Accumulate signed whole-ring area exactly as a two-word integer. Biased
// corner products fit uint32; carry/borrow retains the arbitrary-length sum.
// This avoids depending on optional GPU 64-bit integer arithmetic.
inline uint2 nm_face_area_add(uint2 a,uint positive,uint negative) {
 uint old=a.x;a.x+=positive;a.y+=a.x<old?1u:0u;old=a.x;a.x-=negative;a.y-=old<negative?1u:0u;return a;
}
inline int nm_face_edge_x(int x,int y,int step,int row) {return (x*65536+65535+step*(row-y))/65536;}

kernel void nm_face_prepare(device const nm_face_Corner* corners [[buffer(0)]],device const uint* offsets [[buffer(1)]],device const float4x4* poses [[buffer(2)]],constant float4* u [[buffer(3)]],device const nm_face_Face* faces [[buffer(4)]],device const nm_face_Visual* visuals [[buffer(5)]],device const nm_face_Material* materials [[buffer(6)]],device const float4* frames [[buffer(7)]],device const nm_face_Rules* rules [[buffer(10)]],device const nm_face_Selected* selected [[buffer(11)]],device const uint* materialOffsets [[buffer(12)]],device const nm_face_Slot* slots [[buffer(13)]],device const uint* selectors [[buffer(14)]],device const float4* waterObjects [[buffer(18)]],device nm_face_Prepared* output [[buffer(21)]],device const uint* bases [[buffer(22)]],constant nm_face_Control& c [[buffer(23)]],device const nm_projection_Record* projection [[buffer(27)]],device const uint* projectionFlags [[buffer(28)]],constant float4& projectionView [[buffer(29)]],uint at [[thread_position_in_grid]]) {
 uint ordinal=at%c.span.z,iid=c.span.x+at/c.span.z;
 nm_face_Face f=faces[ordinal];nm_face_Prepared p={};p.pixel=float4(1,1,0,0);p.control.w=f.cornerCount;
 uint dest=bases[iid]-1+ordinal;float4x4 m=poses[offsets[iid]+f.piece];
 if(m[3].w<.5){output[dest]=p;return;}
 nm_face_Visual visual=visuals[iid];float4 uv=float4(0);bool admitted=true;
 if(f.material){nm_face_Material material=materials[f.material];uint frame=material.kind==1?min(uint(max(visual.state.x,0.f)),max(material.frameCount,1u)-1):0;uv=frames[material.firstFrame+frame];
  if(u[4].x>.5&&materialOffsets[iid]){nm_face_Selected s=selected[materialOffsets[iid]-1+material.reserved];uv=s.uv;admitted=s.params.x>=.5;}
 }
 if(c.span.w){uint slot=selectors[iid];if(!slot){output[dest]=p;return;}nm_face_Slot placement=slots[slot-1];p.pixel.xy=placement.atlas.zw/placement.screen.zw;p.pixel.zw=-placement.screen.xy*p.pixel.xy;}
 // Keyed units and model features quantize in production record space;
 // unknown keyless/cache staging explicitly retains the previous projection.
 // UV lanes are original selected-frame texel endpoints, not normalized
 // interpolation between triangles. Sampling floors(lanes+1/65536) (§11.2).
 // Authored corner UVs interpolate between those endpoints.
 uv=round(uv*c.texture.xyxy-.5);
 float keyOrigin=m[2].w>.5?m[1].w:visual.bounds.w;
 int bias=u[7].w>.5?int(rules[iid].clip.w):0;
 bool shaded=!(m[0].w>.5&&m[0].w<1.5);uint2 area=uint2(0);int2 first=0,last=0;bool fits=true,high=false;float2 sum=0;uint top=0,bottom=0;int minY=0,maxY=0,minX=0,maxX=0;
 int4 xx=0,yy=0,keys=0,shade=0;float4 tu=0,tv=0;
 for(uint i=0;i<f.cornerCount;i++){
  nm_face_Corner v=corners[f.cornerStart+i];float3 w=(m*float4(float3(v.p),1)).xyz;if(c.texture.z>.5&&w.y>waterObjects[iid].y)high=true;float2 pixel=floor(nm_projection_screen(w,projection[iid],projectionFlags[offsets[iid]+f.piece],projectionView,nm_face_screen(w,u))*p.pixel.xy+p.pixel.zw)+8192;
  if(!all(isfinite(pixel))||any(pixel<0)||any(pixel>65535)){fits=false;continue;}
  sum+=pixel-8192;int2 xy=int2(pixel);if(i==0){first=xy;minY=maxY=xy.y;minX=maxX=xy.x;}else{area=nm_face_area_add(area,uint(last.x)*uint(xy.y),uint(last.y)*uint(xy.x));if(xy.y<minY){minY=xy.y;top=i;}if(xy.y>maxY){maxY=xy.y;bottom=i;}minX=min(minX,xy.x);maxX=max(maxX,xy.x);}last=xy;
  if(f.cornerCount==4){float2 t=mix(uv.xy,uv.zw,v.uv);tu[i]=t.x;tv[i]=t.y;xx[i]=xy.x;yy[i]=xy.y;keys[i]=int(floor(w.y-keyOrigin))+bias;float3 normal=(m*float4(float3(v.shadeNormal),0)).xyz*float3(1,1,-1);shade[i]=m[0].w>1.5?15:int(dot(normal,float3(-.8,1,.25))*5)&31;}
 }
 p.centroid.xy=f.cornerCount?sum/float(f.cornerCount):float2(0);
 area=nm_face_area_add(area,uint(last.x)*uint(first.y),uint(last.y)*uint(first.x));
 bool front=(area.y&0x80000000u)==0&&any(area!=uint2(0));p.control.z=(admitted&&fits&&front?1u:0u)|(shaded?4u:0u)|(!fits?8u:0u)|(high?16u:0u);p.control.y=top;
 // Admission is the original whole integer ring, even for subdivided/folded
 // faces. Never substitute triangle winding or a transformed-normal test.
 if(f.cornerCount==4&&fits&&minY<maxY&&maxX<=32767&&maxX-minX<32768&&minY<16384&&all(keys>=-32768)&&all(keys<=32767)){
  uint mm=(bottom-top)&3;p.control.x=mm;
  for(uint j=0;j<4;j++){uint i=(top+j)&3;p.x[j]=xx[i];p.y[j]=yy[i];p.key[j]=keys[i];p.shade[j]=shade[i];p.u[j]=tu[i];p.v[j]=tv[i];}
  for(uint e=0;e<4;e++){uint from=e<mm?e:((e+1)&3),to=e<mm?((e+1)&3):e;int dy=p.y[to]-p.y[from];p.step[e]=dy>0?((p.x[to]-p.x[from])*65536)/dy:0;}
  p.control.z|=2;
 }
 output[dest]=p;
}

// Production two-chain mapper [03 R-RAST-01 §1], with later matching edges
// overriding earlier edges on folded chains. This is not inverse bilinear UV.
inline float4 nm_face_lanes(nm_face_Prepared p,float2 pixel) {
 float row=floor(pixel.y)+8192,col=floor(pixel.x)+8192;int mm=int(p.control.x);float lx=p.x.x,rx=lx;
 float4 lanes[4];
 for(uint j=0;j<4;j++)lanes[j]=float4(p.u[j],p.v[j],float(p.key[j]),float(p.shade[j]));
 float4 ll=lanes[0],rl=ll;
 for(int k=0;k<3;k++){
  int ls=k==0?0:4-k,le=3-k;
  if(le>=mm&&p.y[le]>p.y[ls]&&row>=p.y[ls]&&row<p.y[le]){float t=(row-p.y[ls])/float(p.y[le]-p.y[ls]);ll=lanes[ls]+(lanes[le]-lanes[ls])*t;lx=nm_face_edge_x(p.x[ls],p.y[ls],p.step[le],int(row));}
  int re=k+1;
  if(k<mm&&p.y[re]>p.y[k]&&row>=p.y[k]&&row<p.y[re]){float t=(row-p.y[k])/float(p.y[re]-p.y[k]);rl=lanes[k]+(lanes[re]-lanes[k])*t;rx=nm_face_edge_x(p.x[k],p.y[k],p.step[k],int(row));}
 }
 float t=rx>lx?clamp((col-lx)/(rx-lx),0.f,1.f):0.f;return ll+(rl-ll)*t;
}
inline float2 nm_face_uv(float4 lanes,float2 atlasSize){return (floor(lanes.xy+1.f/65536.f)+.5)/atlasSize;}
inline float nm_face_key(float4 lanes){float k=floor(lanes.z);return k-floor(k/256)*256;}

inline bool nm_face_admitted(nm_face_Prepared p){return (p.control.z&1u)!=0;}
inline bool nm_face_mapped(nm_face_Prepared p){return (p.control.z&2u)!=0;}
inline float nm_face_shade(nm_face_Prepared p,float4 lanes){return (p.control.z&4u)!=0?floor(lanes.w):-1.f;}
// TODO(retained-quad-rounding): production's separate Kage key and colour
// passes have documented compiler-dependent last-bit differences (§22.4).
// A Metal caller using one mapped key does not reproduce those self-rejections.
// Compare bounded captures before claiming cross-compiler bit equivalence.
