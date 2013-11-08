#!/usr/bin/env python

import os
import sys
import json
from urllib2 import urlopen

def verify_team(org_name, team_name, api_response):
    for team in api_response:
        if team['name'] == team_name and team['organization']['login'] == org_name:
            return True
    return False

if __name__ == '__main__':
    data = json.loads(urlopen("https://api.github.com/user/teams?access_token={0}".format(os.environ['AUTH_TOKEN'])).read())
    if not verify_team("RealGeeks", "Geeks", data):
        sys.exit(1)
