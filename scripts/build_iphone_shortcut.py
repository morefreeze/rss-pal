#!/usr/bin/env python3
"""Build the token-free share shortcut; sign the resulting plist on macOS.

python3 scripts/build_iphone_shortcut.py /tmp/rss-pal-unsigned.shortcut
shortcuts sign --mode anyone --input /tmp/rss-pal-unsigned.shortcut \
  --output frontend/public/shortcuts/加入RSSPal.shortcut
"""
import plistlib
import sys
from pathlib import Path

ORIGIN = "https://rss.morefreeze.top"
ENCODE_ID = "DCA5D2A1-1B45-4BBD-B6A9-131228574F20"


def action(identifier, **params):
    return {"WFWorkflowActionIdentifier": f"is.workflow.actions.{identifier}",
            "WFWorkflowActionParameters": params}


def workflow():
    prefix = f"{ORIGIN}/capture?url="
    encoded_url = {
        "WFSerializationType": "WFTextTokenString",
        "Value": {
            "string": prefix + "\ufffc",
            "attachmentsByRange": {
                f"{{{len(prefix)}, 1}}": {
                    "Type": "ActionOutput", "OutputUUID": ENCODE_ID,
                    "OutputName": "URL Encoded Text",
                },
            },
        },
    }
    return {
        "WFWorkflowName": "加入 RSS Pal",
        "WFWorkflowClientVersion": "900",
        "WFWorkflowMinimumClientVersion": 900,
        "WFWorkflowMinimumClientVersionString": "900",
        "WFWorkflowIcon": {"WFWorkflowIconStartColor": 4282601983, "WFWorkflowIconGlyphNumber": 59511},
        "WFWorkflowTypes": ["ActionExtension"],
        "WFWorkflowInputContentItemClasses": ["WFURLContentItem", "WFStringContentItem", "WFSafariWebPageContentItem"],
        "WFWorkflowOutputContentItemClasses": [],
        "WFWorkflowHasShortcutInputVariables": True,
        "WFWorkflowImportQuestions": [],
        "WFQuickActionSurfaces": [],
        "WFWorkflowActions": [
            action("geturls", WFInput={"WFSerializationType": "WFTextTokenAttachment", "Value": {"Type": "ExtensionInput"}}),
            action("getitemfromlist", WFItemSpecifier="First Item"),
            action("urlencode", WFEncodeMode="Encode", UUID=ENCODE_ID),
            action("url", WFURLActionURL=encoded_url),
            action("openurl"),
        ],
    }


if __name__ == "__main__":
    destination = Path(sys.argv[1])
    destination.write_bytes(plistlib.dumps(workflow(), fmt=plistlib.FMT_BINARY))
    print(f"Wrote credential-free shortcut: {destination}")
