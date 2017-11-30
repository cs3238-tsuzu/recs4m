# recs4m
[![Docker Automated build](https://img.shields.io/docker/automated/tsuzu/recs4m.svg?style=flat-square)]()
[![Docker Build Status](https://img.shields.io/docker/build/tsuzu/recs4m.svg?style=flat-square)]()
- MP3 stream recorder

# Detail
- Record mp3 stream on http protocol
- Then, upload recorded files(ex. Google Play Music/Google Drive)

# Usage
- You should use this with Docker
- $ docker run --name "random_name" -p 8080:80 tsuzu/recs4m
- Access localhost:8080 from a browser
- $ docker exec -ti "random_named" bash
- If you want to upload to Google Play Music, execute gmupload and sign in to Google
- Or,  you want to upload to other platforms, put a script in /root

# License
- Under the MIT License
- Copyright (c) 2017 Tsuzu
