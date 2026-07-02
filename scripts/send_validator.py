import smtplib
from email.message import EmailMessage
import os

# Example script for sending Validator Nodes via Email or SMS Gateways
# Note: You will need to provide your own SMTP credentials or API keys.

def send_validator_email(to_email, smtp_user, smtp_pass):
    msg = EmailMessage()
    msg.set_content(
        "Your The Collective Mobile Validator Node is ready. "
        "Access the Cellular Agent here: https://synthos-collective.local/VALIDATOR_ONBOARDING.html"
    )

    msg['Subject'] = 'Synthos Validator Node Delivery'
    msg['From'] = smtp_user
    msg['To'] = to_email

    try:
        # Example using Gmail's SMTP server
        server = smtplib.SMTP_SSL('smtp.gmail.com', 465)
        server.login(smtp_user, smtp_pass)
        server.send_message(msg)
        server.quit()
        print(f"[SUCCESS] Validator sent to {to_email}")
    except Exception as e:
        print(f"[ERROR] Failed to send to {to_email}: {e}")

if __name__ == "__main__":
    SENDER_EMAIL = os.environ["SENDER_EMAIL"]
    SENDER_PASS = os.environ["SENDER_PASS"]
    
    # You can loop through a trusted contact list here
    contacts = [
        # "15309534064@vtext.com", # Example Verizon SMS Gateway
        # "example@domain.com"
    ]
    
    for contact in contacts:
        send_validator_email(contact, SENDER_EMAIL, SENDER_PASS)
