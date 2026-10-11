struct Uniforms { camera: vec4<f32>, extent: vec4<f32>, timing: vec4<f32>, reserved: vec4<f32> }
@group(0) @binding(0) var<storage,read> endpoints: array<mat4x4<f32>>;
@group(0) @binding(1) var<storage,read_write> poses: array<mat4x4<f32>>;
@group(0) @binding(2) var<uniform> uniforms: Uniforms;
fn quaternion(m:mat4x4<f32>)->vec4<f32>{
 let reflection=vec3<f32>(1.0,1.0,-1.0);
 let x=normalize(m[0].xyz*reflection);
 var y=m[1].xyz*reflection;y=normalize(y-x*dot(x,y));let z=cross(x,y);
 var q:vec4<f32>;let trace=x.x+y.y+z.z;
 if trace>0.0{let s=2.0*sqrt(trace+1.0);q=vec4<f32>((y.z-z.y)/s,(z.x-x.z)/s,(x.y-y.x)/s,s*0.25);}
 else if x.x>y.y && x.x>z.z{let s=2.0*sqrt(1.0+x.x-y.y-z.z);q=vec4<f32>(s*0.25,(y.x+x.y)/s,(z.x+x.z)/s,(y.z-z.y)/s);}
 else if y.y>z.z{let s=2.0*sqrt(1.0+y.y-x.x-z.z);q=vec4<f32>((y.x+x.y)/s,s*0.25,(z.y+y.z)/s,(z.x-x.z)/s);}
 else{let s=2.0*sqrt(1.0+z.z-x.x-y.y);q=vec4<f32>((z.x+x.z)/s,(z.y+y.z)/s,s*0.25,(x.y-y.x)/s);}
 return normalize(q);
}
fn slerp(a:vec4<f32>,input_b:vec4<f32>,t:f32)->vec4<f32>{
 var b=input_b;var cosine=dot(a,b);if cosine<0.0{b=-b;cosine=-cosine;}cosine=clamp(cosine,0.0,1.0);
 if cosine>0.9995{return normalize(mix(a,b,t));}let angle=acos(cosine);
 return normalize((sin((1.0-t)*angle)*a+sin(t*angle)*b)/sin(angle));
}
fn rotation(q:vec4<f32>)->mat4x4<f32>{
 let x=q.x;let y=q.y;let z=q.z;let w=q.w;let reflection=vec3<f32>(1.0,1.0,-1.0);
 return mat4x4<f32>(vec4<f32>(vec3<f32>(1.0-2.0*(y*y+z*z),2.0*(x*y+z*w),2.0*(x*z-y*w))*reflection,0.0),
 vec4<f32>(vec3<f32>(2.0*(x*y-z*w),1.0-2.0*(x*x+z*z),2.0*(y*z+x*w))*reflection,0.0),
 vec4<f32>(vec3<f32>(2.0*(x*z+y*w),2.0*(y*z-x*w),1.0-2.0*(x*x+y*y))*reflection,0.0),vec4<f32>(0.0,0.0,0.0,1.0));
}
@compute @workgroup_size(64) fn interpolate(@builtin(global_invocation_id) id:vec3<u32>){
 let at=id.x;let count=u32(uniforms.timing.y);if at>=count{return;}
 let a=endpoints[at];let b=endpoints[at+count];if a[3].w<0.5 || b[3].w<0.5{poses[at]=mat4x4<f32>();return;}
 let t=clamp(uniforms.timing.x,0.0,1.0);var m=a;
 if t>=1.0{m=b;}else if t>0.0{
  let same=all(a[0].xyz==b[0].xyz)&&all(a[1].xyz==b[1].xyz)&&all(a[2].xyz==b[2].xyz);
  if !same{m=rotation(slerp(quaternion(a),quaternion(b),t));}
  var position=b[3].xyz;if !all(a[3].xyz==b[3].xyz){position=mix(a[3].xyz,b[3].xyz,t);}
  m[3]=vec4<f32>(position,b[3].w);
 }
 m[0]=vec4<f32>(m[0].xyz,b[0].w);m[3]=vec4<f32>(m[3].xyz,b[3].w);
 var origin=b[1].w;if t<=0.0{origin=a[1].w;}else if t<1.0 && a[1].w!=b[1].w{origin=mix(a[1].w,b[1].w,t);}
 m[1]=vec4<f32>(m[1].xyz,origin);m[2]=vec4<f32>(m[2].xyz,select(0.0,1.0,a[2].w>0.5&&b[2].w>0.5));poses[at]=m;
}
