#include <metal_stdlib>
using namespace metal;

// Mirrors meshscene.RetainedLight: four 16-byte lanes. All physical coordinates
// are record X/unsheared Y/height; projection is Y-height/2. No simulation input.
struct nm_light_source {
    float4 position_radius;
    float4 color_ground_gain;
    float4 ground_age_fade_kind;
    uint4 flags;
};
struct nm_light_subject { uint indices[8]; uint count; };

// Exact production subject selection: invoke once per subject, supplying its
// projected record-space center and conservative extent, not per fragment.
inline nm_light_subject nm_light_select(const device nm_light_source *lights,
                                       uint count, float2 center, float extent) {
    nm_light_subject out; out.count = 0;
    float scores[8];
    for (uint j=0; j<count; ++j) {
        nm_light_source light = lights[j];
        if (!light.flags.x) continue;
        float2 d = light.position_radius.xy - float2(0,light.position_radius.z*0.5) - center;
        float radius = light.position_radius.w;
        float reach = radius*1.5 + extent;
        if (dot(d,d)>reach*reach) continue;
        float3 c=light.color_ground_gain.rgb;
        float score=max(c.x,max(c.y,c.z))/(1+dot(d,d)/(radius*radius));
        uint at=out.count;
        if (at==8) {
            at=0;
            for (uint i=1; i<out.count; ++i) if(scores[i]<scores[at]) at=i;
            if(score<=scores[at]) continue;
        } else ++out.count;
        out.indices[at]=j; scores[at]=score;
    }
    return out;
}

// Evaluate a MODEL FACE CENTROID with outward X/Z/height normal, or each
// clipped SMOKE CORNER with smoke=true. Model output is flat across its face;
// smoke corner output is interpolated. Per-pixel/vertex model evaluation would
// change production's response and is not a parity implementation (§23.2).
inline float3 nm_light_irradiance(const device nm_light_source *lights,
                                 nm_light_subject subject, float3 physical,
                                 float3 normal, bool smoke) {
    float3 rgb=0;
    for(uint i=0; i<subject.count; ++i) {
        nm_light_source light=lights[subject.indices[i]];
        float3 d=light.position_radius.xyz-physical;
        float d2=dot(d,d), r2=light.position_radius.w*light.position_radius.w;
        if(d2>=r2) continue;
        float f=1-d2/r2; f*=f;
        float response=smoke ? 0.8 : max(dot(d,normal)/sqrt(max(d2,1.0)),0.0);
        rgb+=light.color_ground_gain.rgb*(f*response*(smoke ? 2.5 : 3.25));
    }
    return rgb;
}

// Production model storage rounds to three base-128 [0,2] channels before
// shading. Smoke keeps unquantized corner irradiance and half source alpha.
inline float3 nm_light_model_quantize(float3 rgb) { return floor(clamp(rgb,0.0,2.0)*63.5+0.5)/63.5; }
inline float3 nm_light_model(float3 albedo,float shade,float3 irradiance) {
    float3 light=nm_light_model_quantize(irradiance), base=albedo*shade;
    if(all(light==float3(0))) return base;
    return min(base+albedo*light,1.0);
}
inline float3 nm_light_smoke(float3 source,float3 irradiance) {
    return min(source+(0.2+0.8*source)*irradiance,1.0);
}

inline float3 nm_light_linear(float3 c) {
    return select(c/12.92,pow((c+0.055)/1.055,float3(2.4)),c>=0.04045);
}
inline float3 nm_light_display(float3 c) {
    return select(c*12.92,1.055*pow(c,float3(1.0/2.4))-0.055,c>=0.0031308);
}
// Linear optical density for the bounded terrain field (§31.8). Sum this
// energy, then field=1-exp(-sum). GroundGain is exported by production CPU
// groundScale; do not apply family, age/fade or strength a second time here.
inline float3 nm_light_ground_energy(nm_light_source light,float2 projected) {
    if(!light.flags.y || light.color_ground_gain.w<=0) return float3(0);
    float2 center=light.position_radius.xy-float2(0,light.position_radius.z*0.5);
    float2 d=projected-center;
    float h=max(light.position_radius.z-light.ground_age_fade_kind.x,0.0);
    float r2=light.position_radius.w*light.position_radius.w, d2=dot(d,d)+h*h;
    if(d2>=r2) return float3(0);
    float f=1-d2/r2; f=f*f*(3-2*f);
    float3 color=light.color_ground_gain.rgb*light.color_ground_gain.w;
    float peak=max(color.x,max(color.y,color.z));
    return nm_light_linear(color/max(peak,0.000001))*peak*(f*2.0);
}
// Resolve onto painted terrain AFTER water/reflections, BEFORE objects/fog.
// Production reconstructs its half-resolution bounded field bilinearly with
// taps clamped to the field rectangle. Root must supply that field sampling;
// this helper supplies the shared color law, not a substitute full-res pass.
inline float4 nm_light_ground_resolve(float4 original,float3 field) {
    if(max(field.x,max(field.y,field.z))<=0) return original;
    float3 base=nm_light_linear(original.rgb/max(original.a,0.000001));
    float3 reflected=mix(base,float3(dot(base,float3(0.2126,0.7152,0.0722))),0.25);
    reflected*=2-reflected;
    return float4(nm_light_display(base+(1-base)*reflected*field)*original.a,original.a);
}

inline float nm_light_lobe(float x) {
    x=clamp(x,0.0,1.0); x*=x; x*=x; x*=x; x*=x; x*=x; return x;
}
// Return the eight-bit glint weight (including the content percentage scale),
// quantized exactly as production; construction replacement bands suppress it.
inline float nm_light_glint_weight(float3 normal,float strength,bool enabled) {
    if(!enabled) return 0;
    float over=nm_light_lobe(0.9246621);
    float g=max(nm_light_lobe(dot(normal,float3(-0.35,-0.15,0.9246621)))-over,0.0)/(1-over);
    return floor(clamp(g*strength,0.0,1.0)*255+0.5);
}
inline float3 nm_light_glint(float3 albedo,float3 lit,float amount) {
    if(amount<0.5) return lit;
    float peak=max(albedo.x,max(albedo.y,albedo.z)), low=min(albedo.x,min(albedo.y,albedo.z));
    float saturation=(peak-low)/max(peak,0.001);
    float mask=smoothstep(0.12,0.35,peak)*(1-smoothstep(0.2,0.65,saturation));
    return min(lit+mix(float3(1),albedo,0.65)*(amount/255.0)*0.48*mask,1.0);
}
inline float nm_light_finish_response(float3 normal) {
    return floor(clamp(dot(normal,float3(-0.35,-0.15,0.9246621)),0.0,1.0)*7+0.5)/7;
}
// Material 1=steel, 2=paint, 0/default=none. Root gates with Finish separately
// from Glint, preserves key/reveal/waterline order, and supplies face response.
inline float3 nm_light_finish(float3 albedo,float3 lit,uint material,float response,bool enabled) {
    if(!enabled || material==0 || material>2) return lit;
    if(material==1) {
        float lobe=response*response; lobe*=lobe;
        float peak=max(albedo.x,max(albedo.y,albedo.z)),low=min(albedo.x,min(albedo.y,albedo.z));
        float saturation=(peak-low)/max(peak,0.001),over=lobe-0.5397751;
        float3 sky=mix(float3(1),float3(0.876,1.021,1.216),(1-smoothstep(0.2,0.65,saturation))*step(0.0,over));
        lit*=1+0.42*over*sky;
    } else {
        float over=response*response-0.7346939;
        lit=lit*(1+0.12*over)+0.075*max(over,0.0);
    }
    return min(lit,1.0);
}
