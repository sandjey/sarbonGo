--
-- PostgreSQL database dump
--


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
-- Name: archived_trips; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.archived_trips (
    id uuid NOT NULL,
    cargo_id uuid NOT NULL,
    offer_id uuid NOT NULL,
    driver_id uuid,
    status character varying(50) NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    archived_at timestamp with time zone DEFAULT now() NOT NULL,
    cancel_reason text,
    cancelled_by_role character varying(20),
    agreed_price numeric(18,2) DEFAULT 0 NOT NULL,
    agreed_currency character varying(3) DEFAULT 'UZS'::character varying NOT NULL
);


--
-- Name: archived_trips archived_trips_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.archived_trips
    ADD CONSTRAINT archived_trips_pkey PRIMARY KEY (id);


--
-- Name: idx_archived_trips_archived_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_archived_trips_archived_at ON public.archived_trips USING btree (archived_at DESC);


--
-- Name: idx_archived_trips_cargo_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_archived_trips_cargo_id ON public.archived_trips USING btree (cargo_id);


--
-- PostgreSQL database dump complete
--


