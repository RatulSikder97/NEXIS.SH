import sys, zipfile, shutil

src = sys.argv[1]
z = zipfile.ZipFile(src, 'r')
settings = z.read('word/settings.xml').decode('utf-8')
z.close()
if '<w:updateFields' not in settings:
    idx = settings.index('>', settings.index('<w:settings')) + 1
    settings = settings[:idx] + '<w:updateFields w:val="true"/>' + settings[idx:]
newsrc = src + '.tmp'
zin = zipfile.ZipFile(src, 'r')
zout = zipfile.ZipFile(newsrc, 'w', zipfile.ZIP_DEFLATED)
for item in zin.infolist():
    data = zin.read(item.filename)
    if item.filename == 'word/settings.xml':
        data = settings.encode('utf-8')
    zout.writestr(item, data)
zin.close()
zout.close()
shutil.move(newsrc, src)
print(f'{src}: updateFields injected')
