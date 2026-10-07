#include <metal_stdlib>
using namespace metal;

// 48-byte CPU ModelProjection. XYZ is already in battleAffine world space:
// its model Z reflection must not be applied a second time [03 R-RAST-01 §2].
struct nm_projection_Record {float4 origin,anchors,control;};

inline bool nm_projection_known(nm_projection_Record p,uint pieceFlags) {
 return (uint(p.origin.w)&1u)!=0 && (pieceFlags&2u)==0;
}

// Return doubled PRODUCTION RECORD coordinates, without viewport constants.
// These are source corners, not physical output pixels. In particular the
// Supersample-off path doubles native quantized corners (§17.5); it does not
// floor a continuously projected world position at the atlas resolution.
inline float2 nm_projection_record2(float3 w,nm_projection_Record p,uint pieceFlags,float4 view) {
 float s=p.control.x;bool doubled=p.control.y>.5;
 bool direct=(uint(p.origin.w)&2u)!=0 || (pieceFlags&1u)!=0;
 if(direct){
  if(doubled)return floor(float2(2*s*(w.x-view.x),s*(2*w.z-w.y-2*view.y)));
  int y=int(floor(w.y));
  return 2*s*float2(floor(w.x)-view.x,floor(w.z)-float(y>>1)-view.y);
 }
 float3 local=w-p.origin.xyz;int y=int(floor(local.y));
 float2 native=s*float2(floor(local.x),floor(local.z)-float(y>>1));
 if(doubled)return 2*native+p.anchors.zw-float2(0,s*float(y&1));
 return 2*(native+p.anchors.xy);
}

inline float2 nm_projection_screen(float3 w,nm_projection_Record p,uint pieceFlags,float4 view,float2 fallbackScreen) {
 if(!nm_projection_known(p,pieceFlags))return fallbackScreen;
 return .5*nm_projection_record2(w,p,pieceFlags,view)*p.control.z+view.zw;
}

// Source-local atlas coordinate: the caller adds the atlas rectangle origin.
// Call this for original authored corners AND the main geometry vertex path.
// The face preparer applies its integer bias/floor only after this mapping.
inline float2 nm_projection_atlas(float3 w,nm_projection_Record p,uint pieceFlags,float4 view,float4 slotScreen,float4 slotAtlas,float2 fallbackScreen) {
 return (nm_projection_screen(w,p,pieceFlags,view,fallbackScreen)-slotScreen.xy)*(slotAtlas.zw/slotScreen.zw);
}

// TODO(retained-transform-rounding): retained affine matrices and quaternion
// interpolation do not reproduce the production rounded 16.16 hierarchy or
// angle-by-angle interpolation. Source-space projection fixes staging, not
// those transform differences (§2.2.1, §13.5).
// TODO(retained-record-raster): scaling record corners into a physical atlas
// does not itself reproduce recording on the 2x record grid followed by a
// separate output transform. Keep source record2 coordinates available to the
// mapper; captures must settle residual non-rest-scale row/UV differences.
