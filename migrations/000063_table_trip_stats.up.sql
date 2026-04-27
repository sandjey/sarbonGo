--
-- PostgreSQL database dump
--

\restrict TsAa56XwxDgWiMIHBPED1KXDMWuHBBxaNg412WUmgwK8YXafLir9d7BYyI23N1v

-- Dumped from database version 18.3
-- Dumped by pg_dump version 18.3

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: trip_stats; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.trip_stats (
    day_utc date NOT NULL,
    role character varying(32) NOT NULL,
    user_id uuid DEFAULT '00000000-0000-0000-0000-000000000000'::uuid NOT NULL,
    started_count bigint DEFAULT 0 NOT NULL,
    completed_count bigint DEFAULT 0 NOT NULL,
    cancelled_count bigint DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: trip_stats trip_stats_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.trip_stats
    ADD CONSTRAINT trip_stats_pkey PRIMARY KEY (day_utc, role, user_id);


--
-- PostgreSQL database dump complete
--

\unrestrict TsAa56XwxDgWiMIHBPED1KXDMWuHBBxaNg412WUmgwK8YXafLir9d7BYyI23N1v

