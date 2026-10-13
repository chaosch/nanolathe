// Presentation-only port of Nanolathe's MIT retained Metal model equations.
// Hardware triangles approximate the authored two-chain span raster. Each
// subject has an isolated height-key plane and one ordered world commit.
// TODO(question): ordinary body hardware outline lines occupy one atlas texel,
// now half a native pixel at 2x. Native row endpoints must replace this path
// to preserve production whole pixel blocks; authored rings/projection permit
// a separate bounded row unit. Independent shadow packing keeps its own scale.
struct Uniforms { camera:vec4<f32>,extent:vec4<f32>,timing:vec4<f32>,projection:vec4<f32>,shadow:vec4<f32> }
struct Vertex { position:vec4<f32>,normal:vec4<f32>,uv:vec2<f32>,piece:u32,material:u32,color:vec4<f32>,shadeNormal:vec4<f32> }
struct Pose { matrix:mat4x4<f32>,flags:vec4<u32> }
struct Instance {
 ids:vec4<u32>,state:vec4<f32>,bounds:vec4<f32>,outline:vec4<f32>,emission:vec4<f32>,
 reveal:vec4<f32>,below:vec4<f32>,band:vec4<f32>,above:vec4<f32>,clip:vec4<f32>,shadow:vec4<f32>,ruleOutline:vec4<f32>,
 origin:vec4<f32>,anchors:vec4<f32>,control:vec4<f32>,screen:vec4<f32>,atlas:vec4<f32>,
 lightIDs0:vec4<u32>,lightIDs1:vec4<u32>,counts:vec4<u32>,
 shadowAtlas:vec4<f32>,shadowParams:vec4<f32>,shadowPlacement:vec4<f32>,shadowScreen:vec4<f32>,
 groupOperand:vec4<i32>,groupClip:vec4<f32>,
 direct:vec4<f32>
}
struct Material { first:u32,count:u32,kind:u32,local:u32 }
struct Selected { uv:vec4<f32>,color:vec4<f32>,params:vec4<f32> }
struct Light { position:vec4<f32>,color:vec4<f32>,age:vec4<f32>,flags:vec4<u32> }
// Each named fixed array is specialized to its allocated buffer capacity.
// The pinned render HAL does not supply runtime-array bounds metadata.
@group(0) @binding(0) var<storage,read> vertices:array<Vertex,1>;
@group(0) @binding(1) var<storage,read> poses:array<Pose,1>;
@group(0) @binding(2) var<storage,read> instances:array<Instance,1>;
@group(0) @binding(3) var<uniform> u:Uniforms;
@group(0) @binding(4) var atlas:texture_2d<f32>;
@group(0) @binding(5) var palette:texture_2d<f32>;
@group(0) @binding(6) var<storage,read> materials:array<Material,1>;
@group(0) @binding(7) var<storage,read> frames:array<vec4<f32>,1>;
@group(0) @binding(8) var<storage,read> selected:array<Selected,1>;
@group(0) @binding(9) var<storage,read> lights:array<Light,1>;
@group(0) @binding(10) var nearestSampler:sampler;
@group(0) @binding(11) var<storage,read> centers:array<vec4<f32>,1>;
struct Out {
 @builtin(position) position:vec4<f32>,@location(0) uv:vec2<f32>,@location(1) color:vec3<f32>,
 @location(2) shade:f32,@location(3) key:f32,@location(4) @interpolate(flat) faceNormal:vec3<f32>,
 @location(5) @interpolate(flat) irradiance:vec3<f32>,@location(6) @interpolate(flat) params:vec4<f32>,
 @location(7) @interpolate(flat) instance:u32
}
fn project(w:vec3<f32>)->vec2<f32> {return vec2<f32>(w.x-u.camera.x,w.z-u.camera.y-w.y*0.5)*u.camera.z+u.extent.xy*0.5;}
fn projection(w:vec3<f32>,i:Instance,flags:u32)->vec2<f32> {
 if (u32(i.origin.w)&1u)==0u || (flags&2u)!=0u {return project(w);}
 let s=i.control.x;let doubled=i.control.y>0.5;let direct=(u32(i.origin.w)&2u)!=0u || (flags&1u)!=0u;
 var record:vec2<f32>;
 if direct {
  if doubled {record=floor(vec2<f32>(2.0*s*(w.x-u.projection.x),s*(2.0*w.z-w.y-2.0*u.projection.y)));}
  else {let y=i32(floor(w.y));record=2.0*s*vec2<f32>(floor(w.x)-u.projection.x,floor(w.z)-f32(y>>1)-u.projection.y);}
 } else {
  let local=w-i.origin.xyz;let y=i32(floor(local.y));let native=s*vec2<f32>(floor(local.x),floor(local.z)-f32(y>>1));
  if doubled {record=2.0*native+i.anchors.zw-vec2<f32>(0.0,s*f32(y&1));} else {record=2.0*(native+i.anchors.xy);}
 }
 return record*0.5*i.control.z+u.projection.zw;
}
fn modelMaterial(v:Vertex,i:Instance)->Selected {
 var out:Selected;out.uv=vec4<f32>(v.uv,v.uv);out.color=vec4<f32>(v.color.xyz,1.0);out.params=vec4<f32>(1.0,0.0,0.0,0.0);
 if v.material!=0u {let mat=materials[v.material];let frame=select(0u,min(u32(max(i.state.x,0.0)),max(mat.count,1u)-1u),mat.kind==1u);let r=frames[mat.first+frame];out.uv=vec4<f32>(mix(r.xy,r.zw,v.uv),vec2<f32>(0.0));
  if i.ids.w>0u {if mat.local>=i.ids.w {out.params.x=0.0;}else {out=selected[i.ids.z+mat.local];out.uv=vec4<f32>(mix(out.uv.xy,out.uv.zw,v.uv),vec2<f32>(0.0));}}
 }
 return out;
}
fn irradiance(i:Instance,physical:vec3<f32>,normal:vec3<f32>)->vec3<f32> {
 var result=vec3<f32>(0.0);
 for(var j=0u;j<i.counts.x;j++){var at=0u;if j<4u{at=i.lightIDs0[j];}else{at=i.lightIDs1[j-4u];}let l=lights[at];let d=l.position.xyz-physical;let d2=dot(d,d);let r2=l.position.w*l.position.w;if d2>=r2{continue;}var fall=1.0-d2/r2;fall*=fall;let response=max(dot(d,normal)/sqrt(max(d2,1.0)),0.0);result+=l.color.rgb*(fall*response*3.25);}
 return floor(clamp(result,vec3<f32>(0.0),vec3<f32>(2.0))*63.5+0.5)/63.5;
}
fn admitted(i:Instance,material:u32,m:mat4x4<f32>,flags:u32)->bool {
 let header=bitcast<vec4<u32>>(centers[i.ids.y+materials[material].local*2u+1u]);if header.y==0u{return true;}
 var first=vec2<f32>(0.0);var previous=vec2<f32>(0.0);var area=0.0;
 for(var k=0u;k<header.y;k++){let world=(m*vec4<f32>(centers[header.x+k].xyz,1.0)).xyz;let pixel=floor((projection(world,i,flags)-i.screen.xy)*(i.atlas.zw/i.screen.zw));if k==0u{first=pixel;}else{let a=previous-first;let b=pixel-first;area+=a.x*b.y-a.y*b.x;}previous=pixel;}
 // Same whole-ring sign as production; arithmetic here is float32 rather
 // than the production integer edge accumulation and remains approximate.
 return area>0.0;
}
@vertex fn model_vertex(@builtin(vertex_index) vid:u32,@builtin(instance_index) iid:u32)->Out {return modelVertex(vid,iid);}
// Outlines follow retail's polygon-outline rows [03 R-RAST-01 §1]: each ring
// of three corners or more walks its two chains down from its top corner;
// on each raster row the chain edge spanning it (half-open in rows; a later
// folded edge overrides) marks the pixel at its 16.16 step, and a row whose
// right pixel is not right of its left marks nothing. One quad per ring
// covers its raster bounds, six vertices (triangles 0,1,2 and 3,4,5): the
// pinned wgpu Metal backend issues every draw as triangles, so outlines
// cannot use a line-list pipeline. The vertex carries the ring's corners as
// flat values and outline_fragment walks the chains.
struct OutlineOut {
 @builtin(position) position:vec4<f32>,@location(0) @interpolate(flat) instance:u32,
 // Corner count, top corner, bottom corner.
 @location(1) @interpolate(flat) ring:vec4<u32>,@location(2) @interpolate(flat) raster:vec3<f32>,
 // Raster x and y biased by 8192 in the low and high halves, then each
 // corner's height key, 16 corners each (modelOutlineCorners).
 @location(3) @interpolate(flat) xy0:vec4<u32>,@location(4) @interpolate(flat) xy1:vec4<u32>,
 @location(5) @interpolate(flat) xy2:vec4<u32>,@location(6) @interpolate(flat) xy3:vec4<u32>,
 @location(7) @interpolate(flat) key0:vec4<f32>,@location(8) @interpolate(flat) key1:vec4<f32>,
 @location(9) @interpolate(flat) key2:vec4<f32>,@location(10) @interpolate(flat) key3:vec4<f32>
}
@vertex fn outline_vertex(@builtin(vertex_index) vid:u32,@builtin(instance_index) iid:u32)->OutlineOut {
 var out:OutlineOut;out.instance=iid;out.position=vec4<f32>(2.0,2.0,2.0,1.0);
 // Rings are (absolute first vertex, corner count), two per centers record.
 let index=vid/6u;let corner=vid%6u;let pair=bitcast<vec4<u32>>(centers[index/2u]);
 let ring=select(pair.xy,pair.zw,(index&1u)!=0u);let i=instances[iid];
 if ring.y<3u||ring.y>16u||i.counts.y==0u{return out;}
 let pose=poses[i.ids.x+vertices[ring.x].piece];let m=pose.matrix;if m[3].w<0.5{return out;}
 let r=outlineRaster(i,pose.flags.x);let origin=select(i.bounds.w,m[1].w,m[2].w>0.5);
 var xy:array<u32,16>;var keys:array<f32,16>;var lo=vec2<f32>(0.0);var hi=vec2<f32>(0.0);var top=0u;var bottom=0u;
 for(var j=0u;j<ring.y;j++){
  let world=(m*vec4<f32>(vertices[ring.x+j].position.xyz,1.0)).xyz;
  let at=round((projection(world,i,pose.flags.x)-r.yz)/r.x);
  // Outside the biased domain, or not finite: the ring is not outlined.
  // Component tests: the pinned SPIR-V writer cannot emit all().
  if !(at.x>=-8192.0&&at.y>=-8192.0&&at.x<=24575.0&&at.y<=24575.0){return out;}
  if j==0u{lo=at;hi=at;}else{if at.y<lo.y{top=j;}if at.y>hi.y{bottom=j;}lo=min(lo,at);hi=max(hi,at);}
  xy[j]=u32(at.x+8192.0)|(u32(at.y+8192.0)<<16u);keys[j]=floor(world.y-origin)+i.clip.w;
 }
 if hi.y<=lo.y{return out;}
 var q=vec2<f32>(lo.x,lo.y);
 if corner==1u||corner==4u||corner==5u{q.x=hi.x+1.0;}
 if corner==2u||corner==3u||corner==5u{q.y=hi.y;}
 let p=outlineToAtlas(i,r,q);
 out.position=vec4<f32>(p.x/u.extent.z*2.0-1.0,1.0-p.y/u.extent.w*2.0,0.5,1.0);
 out.ring=vec4<u32>(ring.y,top,bottom,0u);out.raster=r;
 out.xy0=vec4<u32>(xy[0],xy[1],xy[2],xy[3]);out.xy1=vec4<u32>(xy[4],xy[5],xy[6],xy[7]);
 out.xy2=vec4<u32>(xy[8],xy[9],xy[10],xy[11]);out.xy3=vec4<u32>(xy[12],xy[13],xy[14],xy[15]);
 out.key0=vec4<f32>(keys[0],keys[1],keys[2],keys[3]);out.key1=vec4<f32>(keys[4],keys[5],keys[6],keys[7]);
 out.key2=vec4<f32>(keys[8],keys[9],keys[10],keys[11]);out.key3=vec4<f32>(keys[12],keys[13],keys[14],keys[15]);
 return out;
}
// outlineRaster is the scale and offset taking a raster pixel to a screen
// point: the record projection's own pixels, or screen pixels when the
// instance projects through the camera.
fn outlineRaster(i:Instance,flags:u32)->vec3<f32> {
 if (u32(i.origin.w)&1u)==0u || (flags&2u)!=0u {return vec3<f32>(1.0,0.0,0.0);}
 return vec3<f32>(select(0.5,1.0,i.control.y<=0.5)*i.control.z,u.projection.zw);
}
fn outlineFromAtlas(i:Instance,r:vec3<f32>,p:vec2<f32>)->vec2<f32> {return ((p-i.atlas.xy)*(i.screen.zw/i.atlas.zw)+i.screen.xy-r.yz)/r.x;}
fn outlineToAtlas(i:Instance,r:vec3<f32>,p:vec2<f32>)->vec2<f32> {return (p*r.x+r.yz-i.screen.xy)*(i.atlas.zw/i.screen.zw)+i.atlas.xy;}
fn outlineCorner(v:u32)->vec2<i32> {return vec2<i32>(i32(v&0xffffu)-8192,i32(v>>16u)-8192);}
// outlineChain walks one chain from the top corner to the bottom corner and
// returns (found, pixel x, height key) for raster row y. Biased coordinates
// keep every signed 16.16 product in i32.
fn outlineChain(corners:array<u32,16>,heights:array<f32,16>,ring:vec4<u32>,left:bool,y:i32)->vec3<f32> {
 var xy=corners;var keys=heights;var cur=ring.y;var found=vec3<f32>(0.0);
 for(var n=0u;n<=ring.x;n++){
  var next=cur+1u;if next==ring.x{next=0u;}
  if left{next=ring.x-1u;if cur>0u{next=cur-1u;}}
  let a=outlineCorner(xy[cur]);let b=outlineCorner(xy[next]);
  if b.y>a.y&&y>=a.y&&y<b.y{
   let rows=b.y-a.y;let m=y-a.y;let step=((b.x-a.x)*65536)/rows;
   let x=(((a.x+8192)*65536+65535+step*m)>>16u)-8192;
   let delta=keys[next]-keys[cur];let fraction=f32(m)/f32(rows);
   found=vec3<f32>(1.0,f32(x),floor(keys[cur]+delta*fraction));
  }
  if next==ring.z{break;}
  cur=next;
 }
 return found;
}
fn modelVertex(vid:u32,iid:u32)->Out {
 let v=vertices[vid];let i=instances[iid];let p=poses[i.ids.x+v.piece];let m=p.matrix;var out:Out;out.instance=iid;
 out.position=vec4<f32>(2.0,2.0,2.0,1.0);if m[3].w<0.5||i.counts.y==0u{return out;}
 let world=(m*vec4<f32>(v.position.xyz,1.0)).xyz;let mat=modelMaterial(v,i);if mat.params.x<0.5{return out;}if i.counts.w==0u&&v.material!=0u&&!admitted(i,v.material,m,p.flags.x){return out;}
 var screen=project(world);if i.counts.w==0u{screen=projection(world,i,p.flags.x);}let pixel=(screen-i.screen.xy)*(i.atlas.zw/i.screen.zw)+i.atlas.xy;
 out.position=vec4<f32>(pixel.x/u.extent.z*2.0-1.0,1.0-pixel.y/u.extent.w*2.0,0.5,1.0);out.uv=mat.uv.xy;out.color=mat.color.rgb;out.params=mat.params;
 let origin=select(i.bounds.w,m[1].w,m[2].w>0.5);out.key=floor(world.y-origin)+i.clip.w;
 let shadeNormal=(m*vec4<f32>(v.shadeNormal.xyz,0.0)).xyz*vec3<f32>(1.0,1.0,-1.0);var row=u32(i32(dot(shadeNormal,vec3<f32>(-0.8,1.0,0.25))*5.0))&31u;
 if m[0].w>1.5{row=15u;}out.shade=select(f32(row),-1.0,m[0].w>0.5&&m[0].w<1.5);
 let normal=normalize((m*vec4<f32>(v.normal.xyz,0.0)).xyz);out.faceNormal=normal.xzy;
 if (u32(u.timing.z)&1u)!=0u&&v.material!=0u {let center=(m*vec4<f32>(centers[i.ids.y+materials[v.material].local*2u].xyz,1.0)).xyz;let physical=vec3<f32>((center.x-u.camera.x)*u.camera.z+u.extent.x*0.5,(center.z-u.camera.y)*u.camera.z+u.extent.y*0.5,center.y*u.camera.z);out.irradiance=irradiance(i,physical,out.faceNormal);}
 return out;
}
fn wrappedKey(input:f32)->f32 {let k=floor(input);return k-floor(k/256.0)*256.0;}
fn verdict(i:Instance,key:f32)->vec4<f32> {if key<i.reveal.y{return i.below;}if key>=i.reveal.x{return i.above;}return i.band;}
fn texel(uv:vec2<f32>,blue:bool)->vec4<f32> {
 let tex=textureSample(atlas,nearestSampler,uv);if tex.g>0.5{return vec4<f32>(1.0,1.0,1.0,tex.a);}
 let index=i32(round(tex.r*255.0));return vec4<f32>(textureLoad(palette,vec2<i32>(index,select(0,1,blue)),0).rgb,tex.a);
}
fn clipped(i:Instance,key:f32)->bool {return (i.clip.x>0.5&&i.clip.x<1.5&&key<=i.clip.y)||(i.clip.z>0.5&&key<=i.clip.w);}
fn modelBlue(i:Instance,key:f32)->bool {
 if i.groupOperand.x!=0 {
  if i.groupOperand.y==2{return false;}
  let stored=clamp(key+f32(i.groupOperand.z),0.0,255.0);
  return i.counts.z>0u&&i.groupClip.x>1.5&&stored<=i.groupClip.y;
 }
 return i.counts.z>0u&&i.clip.x>1.5&&key<=i.clip.y;
}
fn lobe(x:f32)->f32 {var a=clamp(x,0.0,1.0);a*=a;a*=a;a*=a;a*=a;a*=a;return a;}
fn finish(source:vec3<f32>,input:vec3<f32>,kind:u32,normal:vec3<f32>)->vec3<f32> {
 if (u32(u.timing.z)&2u)==0u||kind==0u||kind>2u{return input;}
 let response=floor(clamp(dot(normal,vec3<f32>(-0.35,-0.15,0.9246621)),0.0,1.0)*7.0+0.5)/7.0;var lit=input;
 if kind==1u{var r=response*response;r*=r;let peak=max(source.x,max(source.y,source.z));let low=min(source.x,min(source.y,source.z));let saturation=(peak-low)/max(peak,0.001);let over=r-0.5397751;let sky=mix(vec3<f32>(1.0),vec3<f32>(0.876,1.021,1.216),(1.0-smoothstep(0.2,0.65,saturation))*step(0.0,over));lit*=vec3<f32>(1.0)+0.42*over*sky;}else{let over=response*response-0.7346939;lit=lit*(1.0+0.12*over)+vec3<f32>(0.075*max(over,0.0));}return min(lit,vec3<f32>(1.0));
}
fn glint(source:vec3<f32>,lit:vec3<f32>,strength:f32,normal:vec3<f32>)->vec3<f32> {
 if (u32(u.timing.z)&4u)==0u{return lit;}let over=lobe(0.9246621);let g=max(lobe(dot(normal,vec3<f32>(-0.35,-0.15,0.9246621)))-over,0.0)/(1.0-over);let amount=floor(clamp(g*strength,0.0,1.0)*255.0+0.5);if amount<0.5{return lit;}
 let peak=max(source.x,max(source.y,source.z));let low=min(source.x,min(source.y,source.z));let saturation=(peak-low)/max(peak,0.001);let mask=smoothstep(0.12,0.35,peak)*(1.0-smoothstep(0.2,0.65,saturation));return min(lit+mix(vec3<f32>(1.0),source,0.65)*(amount/255.0)*0.48*mask,vec3<f32>(1.0));
}
// Red carries an exact byte key; green distinguishes erased covered pixels
// from untouched atlas space. Staged source erasure retains this plane and
// depth, as production shadeModel does (GPU design §22.4).
struct Fragment { @location(0) color:vec4<f32>,@location(1) metadata:vec4<f32>,@builtin(frag_depth) depth:f32 }
fn modelShade(input:Out)->Fragment {
 let i=instances[input.instance];let key=wrappedKey(input.key);let ruled=i.counts.z>0u;let blue=modelBlue(i,key);
 if ((input.position.xy<i.atlas.xy).x||(input.position.xy<i.atlas.xy).y)||((input.position.xy>=i.atlas.xy+i.atlas.zw).x||(input.position.xy>=i.atlas.xy+i.atlas.zw).y){discard;}
 let tex=texel(input.uv,blue);var erased=tex.a<0.02||(ruled&&clipped(i,key));
 var rule=vec4<f32>(0.0,0.0,0.0,-1.0);if ruled&&i.reveal.z>0.5{rule=verdict(i,key);erased=erased||rule.w< -1.5;}
 if erased&&i.groupOperand.x==0&&i.counts.w==0u{discard;}
 var source=tex.rgb*input.color;if blue&&input.params.x>1.5{source=textureLoad(palette,vec2<i32>(i32(input.params.w),1),0).rgb;}
 let shade=select(min(0.06875*floor(input.shade),2.0),1.0,input.shade<0.0);var color=source*shade;
 if (u32(u.timing.z)&1u)!=0u{if ((input.irradiance!=vec3<f32>(0.0)).x||(input.irradiance!=vec3<f32>(0.0)).y||(input.irradiance!=vec3<f32>(0.0)).z){color=min(color+source*input.irradiance,vec3<f32>(1.0));}}
 if rule.w<0.0{color=finish(source,color,u32(input.params.y),input.faceNormal);color=glint(source,color,input.params.z,input.faceNormal);}else{color=rule.rgb;if blue{color=textureLoad(palette,vec2<i32>(i32(rule.w),1),0).rgb;}}
 let emission=i.emission.rgb*max(i.emission.w,0.0);color=vec3<f32>(1.0)-(vec3<f32>(1.0)-color)*(vec3<f32>(1.0)-clamp(emission,vec3<f32>(0.0),vec3<f32>(1.0)));
 var out:Fragment;out.color=select(vec4<f32>(color*tex.a,tex.a),vec4<f32>(0.0),erased);out.metadata=vec4<f32>(key/255.0,1.0,0.0,0.0);out.depth=select(0.5,(256.0-key)/257.0,ruled&&i.reveal.w>0.5);return out;
}
fn modelOutline(input:OutlineOut)->Fragment {
 let i=instances[input.instance];let outline=select(i.outline,i.ruleOutline,i.counts.z>0u);if i.state.z<=0.0||outline.a<=0.0{discard;}
 if ((input.position.xy<i.atlas.xy).x||(input.position.xy<i.atlas.xy).y)||((input.position.xy>=i.atlas.xy+i.atlas.zw).x||(input.position.xy>=i.atlas.xy+i.atlas.zw).y){discard;}
 let p=vec2<i32>(floor(outlineFromAtlas(i,input.raster,input.position.xy)));
 let xy=array<u32,16>(input.xy0.x,input.xy0.y,input.xy0.z,input.xy0.w,input.xy1.x,input.xy1.y,input.xy1.z,input.xy1.w,input.xy2.x,input.xy2.y,input.xy2.z,input.xy2.w,input.xy3.x,input.xy3.y,input.xy3.z,input.xy3.w);
 let keys=array<f32,16>(input.key0.x,input.key0.y,input.key0.z,input.key0.w,input.key1.x,input.key1.y,input.key1.z,input.key1.w,input.key2.x,input.key2.y,input.key2.z,input.key2.w,input.key3.x,input.key3.y,input.key3.z,input.key3.w);
 let l=outlineChain(xy,keys,input.ring,true,p.y);let r=outlineChain(xy,keys,input.ring,false,p.y);
 if l.x<0.5||r.x<0.5||r.y<=l.y{discard;}
 var raw=r.z;if f32(p.x)==l.y{raw=l.z;}else if f32(p.x)!=r.y{discard;}
 let key=wrappedKey(raw);var color=outline.rgb;if modelBlue(i,key){color=textureLoad(palette,vec2<i32>(i32(i.shadow.w),1),0).rgb;}
 var out:Fragment;out.color=vec4<f32>(color*outline.a,outline.a);out.metadata=vec4<f32>(key/255.0,1.0,0.0,0.0);out.depth=select(0.5,(256.0-key)/257.0,i.counts.z>0u&&i.reveal.w>0.5);return out;
}

@fragment fn model_fragment(input:Out)->Fragment {return modelShade(input);}
@fragment fn outline_fragment(input:OutlineOut)->Fragment {return modelOutline(input);}
struct ModelShadowFragment {@location(0) color:vec4<f32>,@builtin(frag_depth) depth:f32}
// Shadow silhouettes need only coverage and the height key: red carries the
// byte key the commit tests (128 for an unkeyed subject, whose depth is
// fixed), alpha the coverage; depth still orders the faces. This is
// modelShade's alpha, erasure and depth without the palette, lighting,
// finish and glint work.
@fragment fn model_shadow_fragment(input:Out)->ModelShadowFragment {
 let i=instances[input.instance];let key=wrappedKey(input.key);let ruled=i.counts.z>0u;
 if ((input.position.xy<i.atlas.xy).x||(input.position.xy<i.atlas.xy).y)||((input.position.xy>=i.atlas.xy+i.atlas.zw).x||(input.position.xy>=i.atlas.xy+i.atlas.zw).y){discard;}
 let a=textureSample(atlas,nearestSampler,input.uv).a;var erased=a<0.02||(ruled&&clipped(i,key));
 if ruled&&i.reveal.z>0.5{let rule=verdict(i,key);erased=erased||rule.w< -1.5;}
 if erased&&i.groupOperand.x==0&&i.counts.w==0u{discard;}
 let keyed=ruled&&i.reveal.w>0.5;
 var out:ModelShadowFragment;out.color=vec4<f32>(select(128.0,key,keyed)/255.0,0.0,0.0,select(a,0.0,erased));out.depth=select(0.5,(256.0-key)/257.0,keyed);return out;
}
@fragment fn model_shadow_outline(input:OutlineOut)->ModelShadowFragment {let shaded=modelOutline(input);var out:ModelShadowFragment;out.color=vec4<f32>((256.0-round(shaded.depth*257.0))/255.0,0.0,0.0,shaded.color.a);out.depth=shaded.depth;return out;}

// Direct subjects: an opaque single-member subject outside the effects phase
// draws straight into the world in its paint position instead of through the
// isolated 2x body atlas and its resolve (Metal's depth-slab subjects). The
// vertex keeps the staged projection and admission; only the destination moves
// from atlas pixels to world pixels. The fragment reproduces the resolve's
// 2x2 box filter by shading the four sub-sample positions the doubled raster
// would have shaded, so interiors match; silhouettes are single-sampled.
@vertex fn model_direct_vertex(@builtin(vertex_index) vid:u32,@builtin(instance_index) iid:u32)->Out {
 var out=modelVertex(vid,iid);
 if out.position.x>1.5{return out;}
 let atlasPixel=vec2<f32>((out.position.x+1.0)*0.5*u.extent.z,(1.0-out.position.y)*0.5*u.extent.w);
 let i=instances[iid];let screen=(atlasPixel-i.atlas.xy)*(i.screen.zw/i.atlas.zw)+i.screen.xy;
 out.position=vec4<f32>(screen.x/u.extent.x*2.0-1.0,1.0-screen.y/u.extent.y*2.0,0.5,1.0);
 return out;
}
fn directTexel(uv:vec2<f32>,blue:bool)->vec4<f32> {
 let tex=textureSampleLevel(atlas,nearestSampler,uv,0.0);if tex.g>0.5{return vec4<f32>(1.0,1.0,1.0,tex.a);}
 let index=i32(round(tex.r*255.0));return vec4<f32>(textureLoad(palette,vec2<i32>(index,select(0,1,blue)),0).rgb,tex.a);
}
fn directSample(input:Out,i:Instance,uv:vec2<f32>)->vec4<f32> {
 let key=wrappedKey(input.key);let ruled=i.counts.z>0u;let blue=modelBlue(i,key);
 let tex=directTexel(uv,blue);var erased=tex.a<0.02||(ruled&&clipped(i,key));
 var rule=vec4<f32>(0.0,0.0,0.0,-1.0);if ruled&&i.reveal.z>0.5{rule=verdict(i,key);erased=erased||rule.w< -1.5;}
 if erased{return vec4<f32>(0.0);}
 var source=tex.rgb*input.color;if blue&&input.params.x>1.5{source=textureLoad(palette,vec2<i32>(i32(input.params.w),1),0).rgb;}
 let shade=select(min(0.06875*floor(input.shade),2.0),1.0,input.shade<0.0);var color=source*shade;
 if (u32(u.timing.z)&1u)!=0u{if ((input.irradiance!=vec3<f32>(0.0)).x||(input.irradiance!=vec3<f32>(0.0)).y||(input.irradiance!=vec3<f32>(0.0)).z){color=min(color+source*input.irradiance,vec3<f32>(1.0));}}
 if rule.w<0.0{color=finish(source,color,u32(input.params.y),input.faceNormal);color=glint(source,color,input.params.z,input.faceNormal);}else{color=rule.rgb;if blue{color=textureLoad(palette,vec2<i32>(i32(rule.w),1),0).rgb;}}
 let emission=i.emission.rgb*max(i.emission.w,0.0);color=vec3<f32>(1.0)-(vec3<f32>(1.0)-color)*(vec3<f32>(1.0)-clamp(emission,vec3<f32>(0.0),vec3<f32>(1.0)));
 return vec4<f32>(color*tex.a,tex.a);
}
struct DirectFragment { @location(0) color:vec4<f32>,@builtin(frag_depth) depth:f32 }
@fragment fn model_direct_fragment(input:Out)->DirectFragment {
 let i=instances[input.instance];
 // One world pixel spans two raster samples per axis: the sub-samples sit a
 // quarter pixel either side of the centre, the top-left one alone when the
 // resolve does not average.
 let dx=dpdx(input.uv)*0.25;let dy=dpdy(input.uv)*0.25;
 var color=directSample(input,i,input.uv-dx-dy);
 if (u32(u.timing.z)&8u)!=0u{
  color=(color+directSample(input,i,input.uv+dx-dy)+directSample(input,i,input.uv-dx+dy)+directSample(input,i,input.uv+dx+dy))*0.25;
 }
 if color.a<0.001{discard;}
 // Each direct subject owns one depth band, nearer for later paint ranks
 // (direct.x bands, this one direct.y); inside it the atlas path's own depth
 // orders its faces, so a ruled subject still resolves by height key.
 let key=wrappedKey(input.key);
 let local=select(0.5,(256.0-key)/257.0,i.counts.z>0u&&i.reveal.w>0.5);
 var out:DirectFragment;out.color=color;out.depth=(i.direct.x-i.direct.y-1.0+local)/i.direct.x;return out;
}
