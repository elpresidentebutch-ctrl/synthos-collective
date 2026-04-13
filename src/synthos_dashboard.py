# synthos_dashboard.py
"""
A simple Flask + Dash dashboard for the Synthos Collective ecosystem.
Run this file after installing requirements: pip install flask dash plotly pandas
"""
import dash
from dash import dcc, html, dash_table
import pandas as pd
from flask import Flask
import pickle

# Load or import your ecosystem state here
# For demo, we'll use a placeholder function

def load_ecosystem():
    # Replace this with actual loading from your simulation
    # For now, just a mockup
    try:
        with open('ecosystem_state.pkl', 'rb') as f:
            eco = pickle.load(f)
        return eco
    except FileNotFoundError:
        return type('Empty', (), {'agents': {}, 'businesses': {}, 'bank': None})()

def get_agent_df(eco):
    data = [
        {
            'Name': a.name,
            'SYN': a.syn_balance,
            'Job': a.job,
            'Skills': ', '.join(a.skills),
            'Team': a.team or '',
            'Business': next((b.name for b in eco.businesses.values() if b.owner == a.name), ''),
        }
        for a in eco.agents.values()
    ]
    return pd.DataFrame(data)

def get_business_df(eco):
    data = [
        {
            'Name': b.name,
            'Owner': b.owner,
            'Product': b.product,
            'Price': b.price,
            'Balance': b.balance,
            'Sales': b.sales,
        }
        for b in eco.businesses.values()
    ]
    return pd.DataFrame(data)

def get_bank_stats(eco):
    if not hasattr(eco, 'bank') or eco.bank is None:
        return {'Bank SYN': 0, 'Collected Taxes': 0}
    return {'Bank SYN': eco.bank.syn_balance, 'Collected Taxes': eco.bank.collected_taxes}

# Flask server
server = Flask(__name__)
app = dash.Dash(__name__, server=server, suppress_callback_exceptions=True)

# Layout
app.layout = html.Div([
    html.H1("Synthos Collective Ecosystem Dashboard"),
    dcc.Tabs([
        dcc.Tab(label='Agents', children=[
            dash_table.DataTable(
                id='agent-table',
                columns=[{"name": i, "id": i} for i in ['Name', 'SYN', 'Job', 'Skills', 'Team', 'Business']],
                data=[],
                page_size=10,
                style_table={'overflowX': 'auto'},
            ),
        ]),
        dcc.Tab(label='Businesses', children=[
            dash_table.DataTable(
                id='business-table',
                columns=[{"name": i, "id": i} for i in ['Name', 'Owner', 'Product', 'Price', 'Balance', 'Sales']],
                data=[],
                page_size=10,
                style_table={'overflowX': 'auto'},
            ),
        ]),
        dcc.Tab(label='Bank', children=[
            html.Div(id='bank-stats'),
        ]),
    ]),
    html.Button('Refresh', id='refresh-btn', n_clicks=0),
    html.Div(id='refresh-output'),
])

@app.callback(
    [
        dash.dependencies.Output('agent-table', 'data'),
        dash.dependencies.Output('business-table', 'data'),
        dash.dependencies.Output('bank-stats', 'children'),
        dash.dependencies.Output('refresh-output', 'children'),
    ],
    [dash.dependencies.Input('refresh-btn', 'n_clicks')]
)
def update_tables(n_clicks):
    eco = load_ecosystem()
    agent_df = get_agent_df(eco)
    business_df = get_business_df(eco)
    bank_stats = get_bank_stats(eco)
    return (
        agent_df.to_dict('records'),
        business_df.to_dict('records'),
        [html.P(f"{k}: {v}") for k, v in bank_stats.items()],
        f"Refreshed at click {n_clicks}"
    )

if __name__ == '__main__':
    app.run_server(debug=True)
