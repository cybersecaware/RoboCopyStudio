#!/usr/bin/env python3
"""Dependency-free validation of the actual executable's PE resource tree."""
import json
from pathlib import Path
import struct
import sys
import xml.etree.ElementTree as ET

p = Path(sys.argv[1] if len(sys.argv)>1 else 'bin/RoboCopyGUI.exe')
b = p.read_bytes()
u16 = lambda n: struct.unpack_from('<H',b,n)[0]
u32 = lambda n: struct.unpack_from('<I',b,n)[0]
assert b[:2] == b'MZ'
pe=u32(0x3c)
assert b[pe:pe+4] == b'PE\0\0'
assert u16(pe+4)==0x8664, 'not AMD64'
opt=pe+24
assert u16(opt)==0x20b, 'not PE32+'
assert u16(opt+68)==2, 'not Windows GUI subsystem'
sects=opt+u16(pe+20)
def offset(rva):
    for i in range(u16(pe+6)):
        s=sects+i*40
        size,base,rawsize,raw=struct.unpack_from('<IIII',b,s+8)
        if base<=rva<base+max(size,rawsize): return raw+rva-base
    raise ValueError(f'unmapped RVA {rva}')
rva,size=struct.unpack_from('<II',b,opt+112+2*8)
base=offset(rva)
def entries(rel):
    d=base+rel
    n=u16(d+12)+u16(d+14)
    return [struct.unpack_from('<II',b,d+16+i*8) for i in range(n)]
resources={}
for ident,target in entries(0):
    for name,langdir in entries(target&0x7fffffff):
        for language,data in entries(langdir&0x7fffffff):
            ptr,length=struct.unpack_from('<II',b,base+data)
            resources.setdefault(ident,{})[name]=b[offset(ptr):offset(ptr)+length]
manifest=resources.get(24,{}).get(1)
assert manifest, 'RT_MANIFEST missing'
xml=ET.fromstring(manifest)
requested=xml.find('.//{urn:schemas-microsoft-com:asm.v3}requestedExecutionLevel')
assert requested is not None and requested.attrib['level']=='asInvoker'
assert requested.attrib['uiAccess']=='false'
assert b'Microsoft.Windows.Common-Controls' in manifest
group=resources.get(14,{}).get(2)
assert group, 'application RT_GROUP_ICON ID 2 missing'
reserved,kind,count=struct.unpack_from('<HHH',group)
assert reserved==0 and kind==1 and len(group)==6+count*14
sizes=[]
for i in range(count):
    w,h,colors,reserved,planes,bits,length,icon_id=struct.unpack_from('<BBBBHHIH',group,6+i*14)
    w,h=w or 256,h or 256
    image=resources.get(3,{}).get(icon_id)
    assert image and len(image)==length, 'icon group references missing/truncated frame'
    assert planes==1 and bits==32 and reserved==0
    assert image[:8]==b'\x89PNG\r\n\x1a\n'
    assert struct.unpack_from('>II',image,16)==(w,h)
    assert image[24:26]==bytes((8,6)), 'icon frame must have 8-bit RGBA transparency'
    assert w==h
    sizes.append(w)
assert sorted(sizes)==[16,20,24,32,40,48,64,96,128,256], 'missing DPI icon frames'
print(json.dumps({'file':str(p.resolve()),'machine':'AMD64','subsystem':'Windows GUI','manifest_resource':24,'manifest_id':1,'execution_level':requested.attrib['level'],'common_controls_v6':True,'icon_group_id':2,'icon_sizes':sorted(sizes),'bytes':len(b)},indent=2))
